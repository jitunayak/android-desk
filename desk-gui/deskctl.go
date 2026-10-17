package main

// deskctl.go drives scrcpy and adb for the Desk window.
//
// It is the Go counterpart of the `desk` CLI in the parent directory, and the
// two agree on the tricks that make this work:
//
//   - scrcpy's --new-display creates a virtual display owned by the scrcpy
//     server. The system starts the phone's secondary-display launcher on it
//     by itself, so nothing has to be installed on the device.
//   - The logical display id comes from scrcpy's own log ("New display: ...
//     (id=N)"); input and `am start --display` use it.
//   - `screencap -d` refuses logical ids for a display owned by another
//     process, so screenshots use the *physical* id from
//     `dumpsys SurfaceFlinger --display-id`.
//   - `am start --windowingMode` parses its value as an integer, so freeform
//     is 5, not "freeform".

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// appInfo is one launchable app on the phone.
type appInfo struct {
	Name string
	Pkg  string
}

// Android key codes the navigation buttons use.
const (
	keyBack    = 4
	keyHome    = 3
	keyRecents = 187
	keyShades  = 83 // notification panel
)

// windowingModeFreeform is ActivityInfo.WINDOWING_MODE_FREEFORM.
const windowingModeFreeform = 5

var (
	newDisplayRe = regexp.MustCompile(`New display:.*?\(id=(\d+)\)`)
	appLineRe    = regexp.MustCompile(`^\s*\*\s+(.+?)\s{2,}([A-Za-z0-9_.]+)\s*$`)
)

// ctl owns the scrcpy process and the adb connection. Its methods are safe to
// call from any goroutine.
type ctl struct {
	mu       sync.Mutex
	adb      string
	scrcpy   string
	proc     *exec.Cmd
	done     chan struct{}
	logPath  string
	display  int
	physical string
}

// startOpts describes the virtual display to create.
type startOpts struct {
	Size, DPI   string
	Bitrate     string // scrcpy's --video-bit-rate, as in "16M"
	WindowX     int
	WindowY     int
	WindowWidth int
	Title       string
}

func isExec(path string) bool {
	st, err := os.Stat(path)
	return err == nil && !st.IsDir() && st.Mode()&0o111 != 0
}

// findTools locates adb and scrcpy: $DESK_SCRCPY_DIR, the binaries bundled
// with the app, a scrcpy release folder, then PATH.
func (c *ctl) findTools() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.adb != "" && c.scrcpy != "" {
		return nil
	}

	var dirs []string
	if d := os.Getenv("DESK_SCRCPY_DIR"); d != "" {
		dirs = append(dirs, d)
	}
	if exe, err := os.Executable(); err == nil {
		exe, err = filepath.EvalSymlinks(exe)
		dir := filepath.Dir(exe)
		dirs = append(dirs,
			// inside an app bundle: Desk.app/Contents/Resources/bin/<exe>
			filepath.Join(filepath.Dir(filepath.Dir(dir)), "Resources", "bin"),
			filepath.Join(dir, "bin"), // a plain folder build
		)
	}
	if wd, err := os.Getwd(); err == nil {
		dirs = append(dirs, filepath.Join(wd, "bin"), filepath.Join(filepath.Dir(wd), "bin"))
	}
	if home, err := os.UserHomeDir(); err == nil {
		for _, pat := range []string{
			filepath.Join(home, "Downloads", "scrcpy-macos-*"),
			filepath.Join(home, "Downloads", "scrcpy-linux-*"),
			filepath.Join(home, "Downloads", "scrcpy-win*"),
			filepath.Join(home, "scrcpy*"),
			"/opt/scrcpy",
		} {
			matches, _ := filepath.Glob(pat)
			// Newest release folder first.
			sort.Sort(sort.Reverse(sort.StringSlice(matches)))
			dirs = append(dirs, matches...)
		}
	}

	for _, d := range dirs {
		if c.adb == "" {
			if p := filepath.Join(d, "adb"); isExec(p) {
				c.adb = p
			}
		}
		if c.scrcpy == "" {
			if p := filepath.Join(d, "scrcpy"); isExec(p) {
				c.scrcpy = p
			}
		}
	}
	if c.adb == "" {
		c.adb, _ = exec.LookPath("adb")
	}
	if c.scrcpy == "" {
		c.scrcpy, _ = exec.LookPath("scrcpy")
	}

	switch {
	case c.adb == "":
		return errors.New("adb not found: install scrcpy (it bundles adb) or set DESK_SCRCPY_DIR")
	case c.scrcpy == "":
		return errors.New("scrcpy not found: install it or set DESK_SCRCPY_DIR")
	}
	return nil
}

func (c *ctl) adbRun(args ...string) (string, error) {
	if err := c.findTools(); err != nil {
		return "", err
	}
	out, err := exec.Command(c.adb, args...).CombinedOutput()
	s := string(out)
	if err != nil {
		return s, fmt.Errorf("adb %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(s))
	}
	return s, nil
}

func (c *ctl) shell(args ...string) (string, error) {
	return c.adbRun(append([]string{"shell"}, args...)...)
}

// device returns the first connected device's serial and model.
func (c *ctl) device() (serial, model string, err error) {
	out, err := c.adbRun("devices")
	if err != nil {
		return "", "", err
	}
	for _, line := range strings.Split(out, "\n")[1:] {
		f := strings.Fields(line)
		if len(f) >= 2 && f[1] == "device" {
			serial = f[0]
			break
		}
	}
	if serial == "" {
		return "", "", errors.New("no device connected — plug the phone in over USB with USB debugging on")
	}
	m, _ := c.shell("getprop", "ro.product.model")
	return serial, strings.TrimSpace(m), nil
}

func scanDisplayID(logPath string) int {
	data, err := os.ReadFile(logPath)
	if err != nil {
		return 0
	}
	all := newDisplayRe.FindAllSubmatch(data, -1)
	if len(all) == 0 {
		return 0
	}
	n, _ := strconv.Atoi(string(all[len(all)-1][1]))
	return n
}

// resolvePhysical reads the virtual display's physical id, which screencap
// needs. It is empty while no virtual display exists.
func (c *ctl) resolvePhysical() string {
	out, err := c.shell("dumpsys", "SurfaceFlinger", "--display-id")
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(out, "\n") {
		if !strings.Contains(strings.ToLower(line), "scrcpy") {
			continue
		}
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "Display" {
			return f[1]
		}
	}
	return ""
}

// start launches scrcpy with a virtual display and waits for it to come up.
func (c *ctl) start(o startOpts) error {
	if err := c.findTools(); err != nil {
		return err
	}
	c.mu.Lock()
	busy := c.proc != nil
	c.mu.Unlock()
	if busy {
		return errors.New("a display is already running")
	}
	if _, _, err := c.device(); err != nil {
		return err
	}

	size := o.Size
	if size == "" {
		size = "1920x1080"
	}
	dpi := o.DPI
	if dpi == "" {
		dpi = "220"
	}

	args := []string{"--new-display=" + size + "/" + dpi, "--no-vd-destroy-content"}
	if o.Bitrate != "" {
		args = append(args, "--video-bit-rate="+o.Bitrate)
	}
	if o.Title != "" {
		args = append(args, "--window-title="+o.Title)
	}
	if o.WindowWidth > 0 {
		args = append(args, fmt.Sprintf("--window-width=%d", o.WindowWidth))
	}
	if o.WindowX != 0 || o.WindowY != 0 {
		args = append(args, fmt.Sprintf("--window-x=%d", o.WindowX), fmt.Sprintf("--window-y=%d", o.WindowY))
	}

	logPath := filepath.Join(os.TempDir(), "desk-gui-scrcpy.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		return err
	}

	cmd := exec.Command(c.scrcpy, args...)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	cmd.Dir = filepath.Dir(c.scrcpy) // scrcpy-server sits beside the scrcpy binary
	cmd.Env = os.Environ()
	if err := cmd.Start(); err != nil {
		logFile.Close()
		return fmt.Errorf("starting scrcpy: %w", err)
	}
	logFile.Close()

	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()

	// scrcpy prints "New display: WxH/DPI (id=N)" once the display exists.
	var id int
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		if id = scanDisplayID(logPath); id > 0 {
			break
		}
		select {
		case <-done:
			return fmt.Errorf("scrcpy exited before the display appeared:\n%s", tailLog(logPath))
		case <-time.After(250 * time.Millisecond):
		}
	}
	if id == 0 {
		_ = cmd.Process.Kill()
		return fmt.Errorf("timed out waiting for the virtual display (log: %s)", logPath)
	}

	c.mu.Lock()
	c.proc, c.done, c.logPath, c.display = cmd, done, logPath, id
	c.mu.Unlock()

	// The physical id shows up a moment after the display does.
	for i := 0; i < 40; i++ {
		if p := c.resolvePhysical(); p != "" {
			c.mu.Lock()
			c.physical = p
			c.mu.Unlock()
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	return nil
}

func tailLog(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return "(no log)"
	}
	lines := strings.Split(strings.TrimRight(string(data), "\n"), "\n")
	if len(lines) > 12 {
		lines = lines[len(lines)-12:]
	}
	return strings.Join(lines, "\n")
}

// stop ends the scrcpy process, which destroys the virtual display.
func (c *ctl) stop() error {
	c.mu.Lock()
	proc, done := c.proc, c.done
	c.mu.Unlock()
	if proc == nil || proc.Process == nil {
		return nil
	}

	_ = proc.Process.Signal(os.Interrupt)
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		_ = proc.Process.Kill()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	}

	c.mu.Lock()
	c.proc, c.done, c.display, c.physical = nil, nil, 0, ""
	c.mu.Unlock()
	return nil
}

// isAlive reports whether scrcpy still runs and its display still exists.
func (c *ctl) isAlive() bool {
	c.mu.Lock()
	proc, done := c.proc, c.done
	c.mu.Unlock()
	if proc == nil {
		return false
	}
	select {
	case <-done:
		return false
	default:
	}
	p := c.resolvePhysical()
	if p == "" {
		return false
	}
	c.mu.Lock()
	c.physical = p
	c.mu.Unlock()
	return true
}

func (c *ctl) displayID() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.display
}

// topActivity is the resumed activity on the virtual display, as "pkg/class".
func (c *ctl) topActivity() string {
	d := c.displayID()
	if d == 0 {
		return ""
	}
	out, err := c.shell("dumpsys", "activity", "activities")
	if err != nil {
		return ""
	}
	lines := strings.Split(out, "\n")
	header := fmt.Sprintf("Display #%d ", d)
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), header) {
			start = i
			break
		}
	}
	if start < 0 {
		return ""
	}
	for i := start + 1; i < len(lines); i++ {
		l := lines[i]
		if strings.HasPrefix(strings.TrimSpace(l), "Display #") {
			break
		}
		if k := strings.Index(l, "topResumedActivity=ActivityRecord{"); k >= 0 {
			f := strings.Fields(l[k+len("topResumedActivity=ActivityRecord{"):])
			if len(f) >= 3 {
				return f[2]
			}
		}
	}
	return ""
}

func (c *ctl) input(args ...string) error {
	d := c.displayID()
	if d == 0 {
		return errors.New("no display running")
	}
	_, err := c.shell(append([]string{"input", "-d", strconv.Itoa(d)}, args...)...)
	return err
}

// pressKey sends an Android key code to the virtual display.
func (c *ctl) pressKey(code int) error {
	return c.input("keyevent", strconv.Itoa(code))
}

// launch starts an app on the virtual display with a MAIN/LAUNCHER intent,
// as a freeform window when freeform is set.
func (c *ctl) launch(pkg string, freeform bool) error {
	d := c.displayID()
	if d == 0 {
		return errors.New("no display running")
	}
	args := []string{"am", "start", "--display", strconv.Itoa(d)}
	if freeform {
		args = append(args, "--windowingMode", strconv.Itoa(windowingModeFreeform))
	}
	args = append(args,
		"-a", "android.intent.action.MAIN",
		"-c", "android.intent.category.LAUNCHER",
		"-p", pkg)

	out, err := c.shell(args...)
	if err != nil {
		return err
	}
	if s := strings.TrimSpace(out); strings.Contains(s, "Exception") || strings.Contains(s, "Error:") {
		return errors.New(s)
	}
	return nil
}

func (c *ctl) amStart(args ...string) error {
	d := c.displayID()
	if d == 0 {
		return errors.New("no display running")
	}
	out, err := c.shell(append([]string{"am", "start", "--display", strconv.Itoa(d)}, args...)...)
	if err != nil {
		return err
	}
	if s := strings.TrimSpace(out); strings.Contains(s, "Error") || strings.Contains(s, "Exception") {
		return errors.New(s)
	}
	return nil
}

// openAppDrawer asks the phone's secondary launcher for its all-apps drawer.
func (c *ctl) openAppDrawer() error {
	err := c.amStart("-a", "com.motorola.launcher3.secondarydisplay.action.SECONDARY_DISPLAY_APP_DRAWER2")
	if err != nil {
		return c.amStart("-n", "com.motorola.launcher3/com.android.launcher3.secondarydisplay.SecondaryDisplayAppDrawer2")
	}
	return nil
}

// goHome returns the virtual display to its launcher.
func (c *ctl) goHome() error {
	err := c.amStart("-n", "com.motorola.launcher3/com.android.launcher3.secondarydisplay.SecondaryDisplayLauncher")
	if err != nil {
		return c.pressKey(keyHome)
	}
	return nil
}

// screenshot writes a PNG of the virtual display to path.
func (c *ctl) screenshot(path string) error {
	if err := c.findTools(); err != nil {
		return err
	}
	c.mu.Lock()
	physical := c.physical
	c.mu.Unlock()
	if physical == "" {
		if physical = c.resolvePhysical(); physical == "" {
			return errors.New("the virtual display is not there")
		}
	}

	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()

	cmd := exec.Command(c.adb, "exec-out", "screencap", "-p", "-d", physical)
	cmd.Stdout = f
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("screencap: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// listApps reads the phone's launchable apps, with the names a launcher shows.
// scrcpy does the label lookup; it takes a few seconds.
func (c *ctl) listApps() ([]appInfo, error) {
	if err := c.findTools(); err != nil {
		return nil, err
	}
	out, err := exec.Command(c.scrcpy, "--list-apps").CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil, fmt.Errorf("scrcpy --list-apps: %w", err)
	}

	seen := map[string]bool{}
	var apps []appInfo
	for _, line := range strings.Split(string(out), "\n") {
		m := appLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		info := appInfo{Name: strings.TrimSpace(m[1]), Pkg: strings.TrimSpace(m[2])}
		if !strings.Contains(info.Pkg, ".") || seen[info.Pkg] {
			continue
		}
		seen[info.Pkg] = true
		apps = append(apps, info)
	}
	if len(apps) == 0 {
		return nil, errors.New("could not read the app list from scrcpy")
	}
	sort.Slice(apps, func(i, j int) bool {
		return strings.ToLower(apps[i].Name) < strings.ToLower(apps[j].Name)
	})
	return apps, nil
}
