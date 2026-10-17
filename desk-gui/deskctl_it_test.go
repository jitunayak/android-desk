package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestDeviceIntegration drives the real phone through the whole path the
// window uses. It is skipped unless DESK_IT=1, because it opens a scrcpy
// window, starts apps on a virtual display and closes it again:
//
//	DESK_IT=1 go test -run TestDeviceIntegration -v ./...
func TestDeviceIntegration(t *testing.T) {
	if os.Getenv("DESK_IT") != "1" {
		t.Skip("set DESK_IT=1 to run against the connected phone")
	}

	c := &ctl{}
	if _, model, err := c.device(); err != nil {
		t.Fatalf("device: %v", err)
	} else {
		t.Logf("device: %s", model)
	}

	if err := c.start(startOpts{Size: "1920x1080", DPI: "220", Title: "Desk IT"}); err != nil {
		t.Fatalf("start: %v", err)
	}
	defer func() {
		if err := c.stop(); err != nil {
			t.Errorf("stop: %v", err)
		}
	}()

	if id := c.displayID(); id == 0 {
		t.Fatal("start returned no display id")
	}
	if !c.isAlive() {
		t.Fatal("the display is not alive right after starting")
	}
	t.Logf("display %d is up", c.displayID())

	// The system starts the phone's secondary launcher by itself.
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(c.topActivity(), "SecondaryDisplayLauncher") {
			break
		}
		time.Sleep(time.Second)
	}
	if top := c.topActivity(); !strings.Contains(top, "SecondaryDisplayLauncher") {
		t.Errorf("top activity is %q, want the secondary launcher", top)
	}

	if err := c.openAppDrawer(); err != nil {
		t.Errorf("openAppDrawer: %v", err)
	}
	time.Sleep(2 * time.Second)
	if top := c.topActivity(); !strings.Contains(top, "AppDrawer") {
		t.Errorf("top activity is %q, want the app drawer", top)
	}

	if err := c.launch("com.android.settings", true); err != nil {
		t.Errorf("launch freeform: %v", err)
	}
	time.Sleep(4 * time.Second)
	if top := c.topActivity(); !strings.Contains(top, "com.android.settings") {
		t.Errorf("top activity is %q, want Settings", top)
	}

	if err := c.pressKey(keyBack); err != nil {
		t.Errorf("pressKey: %v", err)
	}
	if err := c.goHome(); err != nil {
		t.Errorf("goHome: %v", err)
	}
	time.Sleep(time.Second)

	// A screenshot of the virtual display, which needs the physical id.
	shot := filepath.Join(t.TempDir(), "shot.png")
	if err := c.screenshot(shot); err != nil {
		t.Errorf("screenshot: %v", err)
	} else if st, err := os.Stat(shot); err != nil {
		t.Errorf("screenshot: %v", err)
	} else if st.Size() < 5000 {
		t.Errorf("screenshot is only %d bytes", st.Size())
	} else {
		t.Logf("screenshot: %d bytes", st.Size())
	}
}
