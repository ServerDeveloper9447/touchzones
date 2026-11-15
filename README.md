# touchzones

A lightweight touchpad gesture daemon for Linux. It turns the edges of your touchpad into discrete sliders (brightness, volume, workspace switching) and adds 4- and 5-finger tap shortcuts.

It reads raw `evdev` multi-touch events directly, with zero desktop environment lock-in.

---

## What it does

- **Right edge**: Swipe up/down to adjust volume (`wpctl`).
- **Left edge**: Swipe up/down to adjust brightness (`brightnessctl`).
- **Top edge**: Swipe left/right for workspace switching (`hyprctl dispatch workspace`).
- **4 & 5 finger taps**: Quick tap shortcuts (e.g. notifications, app launch).
- **Multi-touch protection**: If 2 or more fingers touch down anywhere (e.g. while two-finger scrolling), edge sliders are disabled so you don't accidentally change volume or brightness.

---

## Requirements

1. **Linux on `amd64` or `arm64`**.
2. **Access to `/dev/input/`**: Your user needs to be in the `input` group:
   ```bash
   sudo usermod -aG input $USER
   ```
   *(Log out and back in for this to take effect, or run `newgrp input` in your active shell).*
3. **Helper tools** (depending on what commands you configure):
   - `brightnessctl` (for brightness)
   - `wpctl` (PipeWire volume control)
   - `hyprctl` (if using Hyprland for workspace switching)
   - `libnotify` / `notify-send`

---

## Build

You need Go installed (`>= 1.22`):

```bash
git clone https://github.com/yourusername/touchzones.git
cd touchzones
go build -o touchzones .
```

Move the binary somewhere in your `$PATH`:
```bash
install -m 755 touchzones ~/.local/bin/
# or system-wide:
sudo install -m 755 touchzones /usr/local/bin/
```

---

## Quick Start

1. Generate the default configuration file:
   ```bash
   touchzones init
   ```
   This creates `~/.config/touchzones/touchzones.conf` (or inside `$XDG_CONFIG_HOME/touchzones/` if set).

2. Test it out in your terminal:
   ```bash
   touchzones
   ```
   Slide your finger down the far right edge of your touchpad to lower volume, or left edge for brightness. Press `Ctrl+C` when done.

3. Run in background:
   ```bash
   touchzones -d
   ```

---

## Autostart

### In your window manager / compositor

Because `touchzones` executes desktop actions, the easiest way to run it is through your compositor config:

- **Hyprland** (`~/.config/hypr/hyprland.conf`):
  ```ini
  exec-once = touchzones
  ```

- **Sway** (`~/.config/sway/config`):
  ```ini
  exec touchzones
  ```

- **River** (`~/.config/river/init`):
  ```bash
  riverctl spawn touchzones
  ```

- **X11** (`~/.xinitrc` or WM config):
  ```bash
  touchzones -d
  ```

### Or with a `systemd` user service

If your distro uses systemd and you prefer a managed service:

Create `~/.config/systemd/user/touchzones.service`:
```ini
[Unit]
Description=Touchpad Edge Sliders Daemon
PartOf=graphical-session.target
After=graphical-session.target

[Service]
Type=simple
ExecStart=%h/.local/bin/touchzones
Restart=on-failure
RestartSec=3

[Install]
WantedBy=graphical-session.target
```

Enable and start it:
```bash
systemctl --user daemon-reload
systemctl --user enable --now touchzones
```

---

## Configuration

Configuration is kept in a simple `key = value` format with `#` comments.

Default file location: `~/.config/touchzones/touchzones.conf`

```ini
# Gesture thresholds
edge = 0.06          # Left/right edge zone width (fraction of touchpad, e.g. 0.06 = 6%)
top_edge = 0.08      # Top edge zone height
step = 0.03          # Distance required to trigger one slider tick
max_tap_time = 0.25  # Max duration (in seconds) to count as a tap
max_tap_move = 120   # Movement tolerance before a tap is cancelled

# Slider commands
left_up    = brightnessctl -q set 3%+
left_down  = brightnessctl -q -n1 set 3%-
right_up   = wpctl set-volume -l 1.0 @DEFAULT_AUDIO_SINK@ 2%+
right_down = wpctl set-volume @DEFAULT_AUDIO_SINK@ 2%-
top_right  = hyprctl dispatch workspace e+1
top_left   = hyprctl dispatch workspace e-1

# Tap actions
tap_4 = notify-send "4-finger tap"
tap_5 = notify-send "5-finger tap"
```

Leave any action empty if you don't want it bound to anything:
```ini
tap_5 =
```

You can pass a custom config path at runtime:
```bash
touchzones -c /path/to/custom.conf
```

---

## CLI Options

```
Usage: touchzones [options] [command]

Commands:
    init                        Creates a config file at ~/.config/touchzones/touchzones.conf

Options:
    -c, --config=path/to/file   Use an alternative config file.
    -d, --daemon                Daemonize the process.
    -k, --kill-daemon           Kill running touchzones daemon.
    -r, --reload                Reload configuration of running daemon.
    -v, --version               Shows touchzones version.
```
