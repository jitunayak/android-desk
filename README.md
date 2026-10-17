# desk — Android desktop mode over scrcpy

A small CLI that gives a phone **without** a built-in desktop mode a real desktop
experience on your computer: a virtual display, a launcher, an all-apps drawer,
and apps running as resizable freeform windows.

Built and verified against a **Motorola Edge 70 (Android 17)** over USB from macOS,
using scrcpy's `--new-display` feature.

```
desk start            # create the virtual display + launcher
desk drawer           # open the all-apps drawer
desk open chrome --freeform
```

---

## Desk.app — the desktop app

There is a native macOS app that wraps all of this in one control surface: a
frameless glass bar with the navigation buttons that the virtual display lacks,
an app drawer hanging under it, and scrcpy's window docked below so the whole
thing reads as one desktop.

![Desk: the glass bar with the app drawer over the phone's desktop](desk-gui/docs/glass.png)

```sh
./build.sh              # Desk.app and Desk.dmg, with adb and scrcpy inside
./build.sh --install    # ... and copy it into /Applications
```

It needs nothing else installed: `adb` and `scrcpy` ship inside the bundle. See
[desk-gui/README.md](desk-gui/README.md) for using it, and the `desk` CLI below
for scripting the same thing.

---

## Why this approach

If you came here looking to trigger Motorola **Ready For / Smart Connect** desktop
mode externally, these three routes are dead ends (all verified on-device):

| Route | Why it fails |
| --- | --- |
| Smart Connect PC app on macOS | Motorola ships a Windows-only client. |
| Cast the phone to the Mac (Miracast / Wi-Fi Display) | macOS has **no Wi-Fi Direct** support — AirServer's docs: *"Mac computers from Apple do not have the hardware or drivers required to use Wi-Fi Direct."* Apple only implements AirPlay, so a Mac can never be a Miracast sink. |
| Start Moto's desktop on a scrcpy virtual display | `DesktopModeLauncherActivity` requires `com.motorola.permission.ACCESS_DESKTOP` (`signature\|privileged`), and the engine's desktop components (`DesktopActivityAlias`, `MainActivity`, `MobileDesktopTile`) are disabled by Motorola in a way `pm enable` refuses for non-debuggable apps. |

What **does** work is putting a launcher on a scrcpy virtual display. Modern
Motorola devices ship a secondary-display launcher that the system starts
automatically, plus a full app drawer. Combine that with Android's freeform
window mode and you get a desktop.

---

## Requirements

- `python3` (macOS ships one)
- `adb` and `scrcpy` — the tool finds them automatically in this order:
  1. `$DESK_SCRCPY_DIR`
  2. `./bin`
  3. `~/Downloads/scrcpy-*`
  4. `PATH`
- USB debugging enabled, device authorised
- Android 10+ with `android.software.freeform_window_management`
  (`desk doctor` checks this)

Run **`desk doctor`** first — it validates every prerequisite above.

---

## Building

From a clone of this repository:

```sh
./build.sh              # the CLI, plus Desk.app and Desk.dmg in desk-gui/build
./build.sh --install    # ... and install it: /Applications/Desk.app, `desk` on PATH
./build.sh --test       # gofmt, vet, the Go tests, mygo vet, the CLI itself
./build.sh --docs       # redraw the pictures in these READMEs
./build.sh --clean      # remove what the build wrote
```

`make` is the same script under shorter names: `make`, `make install`,
`make test`, `make docs`, `make clean`, `make help`.

Neither half needs anything installed but `python3`, and a Go toolchain for the
app. Both find the scrcpy release the way `desk` itself does —
`$DESK_SCRCPY_DIR`, then `./bin`, then the newest `~/Downloads/scrcpy-macos-*` —
and the build copies `adb`, `scrcpy` and `scrcpy-server` into `bin/`, so the CLI
in the checkout runs with nothing on `PATH`:

```sh
./build.sh
./desk doctor
```

`--install` links `desk` into `~/.local/bin` as a symlink to this checkout, so it
keeps reading the same `bin/` and writing the same `.desk/` state; it says so if
that folder is not on your `PATH` yet. Set `GO` or `DESK_BIN_DIR` to build with a
different Go, or install the CLI somewhere else.

### Keeping the app in /Applications current

`--install` is also the way to keep the copy in `/Applications` the build you
just made. Re-run it after a change rather than comparing the two by eye. Go
writes the revision it built from into the binary, so two builds of identical
sources still differ once the revision or the uncommitted-changes flag differs —
an app that looks the same by eye can be an older build.

The installed app can say which commit it came from, with nothing but `grep`:

```sh
grep -ao 'vcs\.revision=[0-9a-f]*' /Applications/Desk.app/Contents/MacOS/Desk
```

That prints the commit the app was built from. (`go version -m` on the same file
says the same thing with more detail, if Go is on your `PATH`.) If it is not the
revision at the top of the log, the app is behind:

```sh
git log -1 --format=%H        # what the sources say
make install                  # rebuild and put it there
```

---

## Commands

### Session

| Command | Does |
| --- | --- |
| `desk start` | Create the virtual display and wait for the launcher. |
| `desk stop` | Destroy the virtual display. |
| `desk status` | Session state, display ids, top activity. |
| `desk info` | Display ids plus raw escape-hatch commands. |
| `desk doctor` | Check tools, device, and desktop prerequisites. |

`desk start` options: `--size 1920x1080`, `--dpi 220`, `--record out.mp4`,
`--keep-content`, `--max-size N`, `--drawer`, `--no-decorations`,
`--timeout SEC`, `--extra <scrcpy args>`.

> `--no-decorations` also removes the launcher — the system stops starting it.

### Apps

| Command | Does |
| --- | --- |
| `desk drawer` | Open the all-apps drawer on the virtual display. |
| `desk apps [QUERY]` | List installed apps (name + package). `--refresh` re-reads. |
| `desk open QUERY` | Launch an app on the virtual display. |
| `desk open QUERY --freeform` | Launch it as a resizable window. |
| `desk home` | Go back to the launcher home screen. |

`desk open` matches on package or (partial) app name. Ambiguous queries list the
candidates and exit; add `--first` to take the first. `--bounds L,T,R,B` sizes a
freeform window; `--wait` blocks until the launch completes.

### Input and capture

| Command | Does |
| --- | --- |
| `desk shot [file.png]` | Screenshot the virtual display. Defaults to `.desk/shots/`. |
| `desk tap X Y` | Tap on the virtual display. |
| `desk swipe X1 Y1 X2 Y2 [ms]` | Swipe / scroll. |
| `desk key back\|home\|recents\|enter\|…` | Send a key. A raw keycode also works. |
| `desk text "hello"` | Type text. |

---

## How it works

1. `desk start` runs `scrcpy --new-display=WxH/DPI`, which creates a virtual
   display owned by the scrcpy server (running as `shell`).
2. The system sees a new display and starts the `SECONDARY_HOME` activity on it —
   `com.motorola.launcher3/…SecondaryDisplayLauncher`. Nothing to do; it just
   appears. On some phones the display starts empty instead, in which case run
   `desk open <launcher>` or install a launcher.
3. The tool reads the logical display id out of the scrcpy log
   (`New display: … (id=N)`) and the **physical** display id from
   `dumpsys SurfaceFlinger --display-id`. The physical id is needed because
   `screencap -d` refuses logical ids for a display owned by another process
   (`screencap` lives in `screenCapLib` and resolves ids against
   `SurfaceComposerClient`). This is the trick that makes headless screenshots
   of the virtual display possible.
4. Apps launch with
   `am start --display <logical> --windowingMode 5 -a MAIN -c LAUNCHER -p <pkg>`
   (`WINDOWING_MODE_FREEFORM` is `5`; `am` parses this flag as an integer, so
   `--windowingMode freeform` fails with a `NumberFormatException`).
5. Input goes through `input -d <logical> tap|swipe|keyevent`.

Runtime state lives in `.desk/` (`state.json`, `apps.json`, `scrcpy.log`, `shots/`).

---

## Known limitations

- **No navigation bar.** The nav bar window *exists* on the virtual display
  (`type=NAVIGATION_BAR`, 66 px) but renders nothing — Moto's secondary launcher
  provides no nav buttons there. Use scrcpy's own shortcuts instead:
  `MOD+b` / right-click = Back, `MOD+h` / middle-click = Home, `MOD+s` = Recents.
- **The display is transient.** It dies when scrcpy exits. `--keep-content`
  moves running apps back to the phone instead of killing them.
- **Stale app list.** `desk apps` caches to `.desk/apps.json` for 24 h; use
  `--refresh` after installing something.
- **No multi-monitor or snapping.** Freeform windows are placed by the system;
  `--bounds` gives partial control.

`tools/bmpview.py` renders a strip of a screenshot as ASCII — useful for
inspecting what the virtual display is actually drawing.

---

## Quick reference

```bash
desk doctor                                  # verify setup
desk start --size 1920x1080 --dpi 220        # go
desk drawer                                  # all apps
desk open "chrome" --freeform                # floating window
desk shot ~/Desktop/desk.png                 # grab a frame
desk key back                                # or use right-click in scrcpy
desk stop                                    # done
```

---

## Licence

Desk is free software under the **GNU General Public License, version 3**
([LICENSE](LICENSE)).

In plain words: use it, change it, hand it to other people — but not as
something closed. Anyone who ships a modified Desk has to ship its source under
the same terms, so nothing built on this can be locked up. (Selling copies is
allowed; the licence cannot forbid that. But since every buyer is entitled to the
source too, a paywall would buy them nothing.)

It comes with no warranty.
