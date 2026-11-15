//go:build linux

package main

import (
	"bufio"
	"cmp"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"log"
	"math"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/holoplot/go-evdev"
)

const version = "1.0.0"

var (
	edge             = 0.06
	topEdge          = 0.08
	maxTapTime       = 0.25 // seconds
	maxTapMove int32 = 120  // position delta
	stepFactor       = 0.03
)

type Zone int

const (
	ZoneUndecided Zone = iota
	ZoneNone
	ZoneLeft
	ZoneRight
	ZoneTop
)

type Touch struct {
	zone    Zone
	last    float64
	hasLast bool
	acc     float64
	x0      int32
	hasX0   bool
	y0      int32
	hasY0   bool
}

var (
	cmdLeftEdgeUp    = []string{"brightnessctl", "-q", "set", "3%+"}
	cmdLeftEdgeDown  = []string{"brightnessctl", "-q", "-n1", "set", "3%-"}
	cmdRightEdgeUp   = []string{"wpctl", "set-volume", "-l", "1.0", "@DEFAULT_AUDIO_SINK@", "2%+"}
	cmdRightEdgeDown = []string{"wpctl", "set-volume", "@DEFAULT_AUDIO_SINK@", "2%-"}
	cmdTopEdgeLeft   []string
	cmdTopEdgeRight  []string

	taps = map[int][]string{
		4: {},
		5: {},
	}

	fingerCodes = map[evdev.EvCode]int{
		evdev.BTN_TOOL_FINGER:    1,
		evdev.BTN_TOOL_DOUBLETAP: 2,
		evdev.BTN_TOOL_TRIPLETAP: 3,
		evdev.BTN_TOOL_QUADTAP:   4,
		evdev.BTN_TOOL_QUINTTAP:  5,
	}

	//go:embed touchzones.conf.default
	defaultConfig string

	homeDir = cmp.Or(os.Getenv("XDG_CONFIG_HOME"), os.Getenv("HOME"))
)

func init() {
	if runtime.GOOS != "linux" {
		log.Fatalf("touchzones is only supported on Linux (running on %s)\n", runtime.GOOS)
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		log.Fatalf("touchzones is only supported on amd64 and arm64 architectures (running on %s)\n", runtime.GOARCH)
	}
}

func spawn(args []string) {
	if len(args) == 0 || args[0] == "" {
		return
	}
	cmd := exec.Command(args[0], args[1:]...)
	if err := cmd.Start(); err != nil {
		return
	}
	go func() {
		_ = cmd.Wait()
	}()
}

func absInt32(n int32) int32 {
	if n < 0 {
		return -n
	}
	return n
}

func decideZone(t *Touch, maxX, maxY float64) {
	if t.zone != ZoneUndecided || !t.hasX0 || !t.hasY0 {
		return
	}
	fx0 := float64(t.x0)
	fy0 := float64(t.y0)
	if fx0 > maxX*(1.0-edge) {
		t.zone = ZoneRight
		t.last = fy0
		t.hasLast = true
	} else if fx0 < maxX*edge {
		t.zone = ZoneLeft
		t.last = fy0
		t.hasLast = true
	} else if fy0 < maxY*topEdge {
		t.zone = ZoneTop
		t.last = fx0
		t.hasLast = true
	} else {
		t.zone = ZoneNone
		t.last = fy0
		t.hasLast = true
	}
}

func findTouchpad() (*evdev.InputDevice, float64, float64, error) {
	paths, err := evdev.ListDevicePaths()
	if err != nil {
		return nil, 0, 0, fmt.Errorf("listing input devices: %w", err)
	}
	for _, p := range paths {
		if strings.Contains(strings.ToLower(p.Name), "touchpad") {
			dev, err := evdev.Open(p.Path)
			if err != nil {
				continue
			}
			abs, err := dev.AbsInfos()
			if err != nil {
				dev.Close()
				continue
			}
			xInfo, okX := abs[evdev.ABS_MT_POSITION_X]
			yInfo, okY := abs[evdev.ABS_MT_POSITION_Y]
			if !okX || !okY {
				dev.Close()
				continue
			}
			return dev, float64(xInfo.Maximum), float64(yInfo.Maximum), nil
		}
	}
	return nil, 0, 0, fmt.Errorf("no touchpad device with multi-touch support found")
}

func fileExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return !info.IsDir(), nil
	}
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	return false, err
}

func acquireLock() (*os.File, error) {
	runtimeDir := os.Getenv("XDG_RUNTIME_DIR")
	var lockPath string
	if runtimeDir != "" {
		lockPath = path.Join(runtimeDir, "touchzones.lock")
	} else {
		lockPath = path.Join(os.TempDir(), fmt.Sprintf("touchzones-%d.lock", os.Getuid()))
	}

	file, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, fmt.Errorf("could not open lock file: %w", err)
	}

	var lockErr error
	for range 10 {
		lockErr = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if lockErr == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if lockErr != nil {
		file.Close()
		pidB, _ := os.ReadFile(lockPath)
		pid := strings.TrimSpace(string(pidB))
		if pid != "" {
			return nil, fmt.Errorf("touchzones is already running (PID %s)", pid)
		}
		return nil, fmt.Errorf("touchzones is already running")
	}

	_ = file.Truncate(0)
	_, _ = file.Seek(0, 0)
	_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())

	return file, nil
}

func daemonize() {
	if os.Getenv("TOUCHZONES_DAEMON") == "1" {
		return
	}

	var childArgs []string
	for _, arg := range os.Args[1:] {
		if arg != "-d" && arg != "--daemon" {
			childArgs = append(childArgs, arg)
		}
	}

	cmd := exec.Command(os.Args[0], childArgs...)
	cmd.Env = append(os.Environ(), "TOUCHZONES_DAEMON=1")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil

	if err := cmd.Start(); err != nil {
		log.Fatalf("Failed to start daemon: %v\n", err)
	}

	fmt.Printf("Started touchzones in background (PID %d)\n", cmd.Process.Pid)
	os.Exit(0)
}

func parseCmd(val string) []string {
	var args []string
	var current strings.Builder
	inQuote := rune(0)
	escaped := false
	hasToken := false

	for _, r := range val {
		if escaped {
			current.WriteRune(r)
			escaped = false
			hasToken = true
			continue
		}

		if r == '\\' && inQuote != '\'' {
			escaped = true
			continue
		}

		if inQuote != 0 {
			if r == inQuote {
				inQuote = 0
			} else {
				current.WriteRune(r)
			}
			continue
		}

		switch r {
		case '"', '\'':
			inQuote = r
			hasToken = true
		case ' ', '\t', '\n', '\r':
			if hasToken {
				args = append(args, current.String())
				current.Reset()
				hasToken = false
			}
		default:
			current.WriteRune(r)
			hasToken = true
		}
	}

	if hasToken {
		args = append(args, current.String())
	}

	return args
}

func loadConfig(path string) {
	exists, err := fileExists(path)
	if err != nil {
		log.Fatalf("Error while checking configuration: %v\n", err)
	}
	if !exists {
		fmt.Printf("[INFO] No config file found at %s. Using built-in defaults.\n\n", path)
		return
	}

	file, err := os.Open(path)
	if err != nil {
		log.Fatalf("Error while checking configuration: %v", err)
	}
	defer file.Close()
	scn := bufio.NewScanner(file)
	for scn.Scan() {
		line := strings.TrimSpace(scn.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		switch key {
		case "edge":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				edge = v
			}
		case "top_edge":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				topEdge = v
			}
		case "max_tap_time":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				maxTapTime = v
			}

		case "max_tap_move":
			if v, err := strconv.Atoi(val); err == nil {
				maxTapMove = int32(v)
			}

		case "step":
			if v, err := strconv.ParseFloat(val, 64); err == nil {
				stepFactor = v
			}

		case "left_up":
			cmdLeftEdgeUp = parseCmd(val)
		case "left_down":
			cmdLeftEdgeDown = parseCmd(val)
		case "right_up":
			cmdRightEdgeUp = parseCmd(val)
		case "right_down":
			cmdRightEdgeDown = parseCmd(val)
		case "top_left":
			cmdTopEdgeLeft = parseCmd(val)
		case "top_right":
			cmdTopEdgeRight = parseCmd(val)

		case "tap_4":
			taps[4] = parseCmd(val)
		case "tap_5":
			taps[5] = parseCmd(val)
		}
	}
	err = scn.Err()
	if err != nil {
		log.Fatalf("Error while parsing configuration: %v", err)
	}
}

func main() {
	if homeDir == "" {
		fmt.Println("$XDG_CONFIG_HOME and $HOME both are unset")
		os.Exit(1)
	}
	var configDir string
	if os.Getenv("XDG_CONFIG_HOME") != "" {
		configDir = path.Join(homeDir, "touchzones")
	} else {
		configDir = path.Join(homeDir, ".config", "touchzones")
	}
	configPath := path.Join(configDir, "touchzones.conf")
	var verFlag, daemon bool
	var config string
	flag.BoolVar(&verFlag, "version", false, "")
	flag.BoolVar(&verFlag, "v", false, "")
	flag.StringVar(&config, "config", configPath, "")
	flag.StringVar(&config, "c", configPath, "")
	flag.BoolVar(&daemon, "daemon", false, "")
	flag.BoolVar(&daemon, "d", false, "")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: touchzones [options] [command]\n\nCommands:\n")
		w := tabwriter.NewWriter(os.Stderr, 0, 0, 3, ' ', 0)
		fmt.Fprintf(w, "    init\tCreates a config file at ~/.config/touchzones/touchzones.conf\n\n")
		fmt.Fprintf(os.Stderr, "Options:\n")
		fmt.Fprintf(w, "    -c, --config=path/to/file\tUse an alternative config file.\n")
		fmt.Fprintf(w, "    -d, --daemon\tDaemonize the process.\n")
		fmt.Fprintf(w, "    -v, --version\tShows touchzones version.\n")
		w.Flush()
	}
	flag.Parse()

	if verFlag {
		fmt.Printf("Version %s\n", version)
		os.Exit(0)
	}

	if flag.NArg() > 0 {
		cmd := flag.Arg(0)
		if cmd == "init" {
			if flag.NArg() > 1 {
				log.Fatalf("Unexpected argument for 'init': %s\n", flag.Arg(1))
			}
			err := os.MkdirAll(configDir, 0755)
			if err != nil {
				log.Fatalf("Error while creating directory: %v", err)
			}
			err = os.WriteFile(config, []byte(defaultConfig), 0644)
			if err != nil {
				log.Fatalf("Error while creating file: %v", err)
			}
			fmt.Printf("Created config file at %s\n", config)
			os.Exit(0)
		}
		log.Fatalf("Unknown command: %s\nRun 'touchzones --help' for usage.\n", cmd)
	}

	loadConfig(config)

	lockFile, err := acquireLock()
	if err != nil {
		log.Fatalf("Error: %v\n", err)
	}

	dev, maxX, maxY, err := findTouchpad()
	if err != nil {
		lockFile.Close()
		log.Fatalf("Error finding touchpad: %v", err)
	}

	if daemon {
		dev.Close()
		lockFile.Close()
		daemonize()
	}

	defer lockFile.Close()
	defer dev.Close()

	name, _ := dev.Name()
	fmt.Printf("Using touchpad: %s (%s), X max: %.0f, Y max: %.0f\n", name, dev.Path(), maxX, maxY)

	step := maxY * stepFactor
	stepH := maxX * stepFactor

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM)
	go func() {
		<-sigChan
		dev.Close()
		lockFile.Close()
		os.Exit(0)
	}()

	slot := int32(0)
	touches := make(map[int32]*Touch)

	var (
		tapStart float64
		hasTap   bool
		tapPeak  int
		tapMoved bool
	)

	for {
		ev, err := dev.ReadOne()
		if err != nil {
			log.Fatalf("Error reading evdev event: %v", err)
		}

		switch ev.Type {
		case evdev.EV_ABS:
			switch ev.Code {
			case evdev.ABS_MT_SLOT:
				slot = ev.Value

			case evdev.ABS_MT_TRACKING_ID:
				if ev.Value == -1 {
					delete(touches, slot)
				} else {
					touches[slot] = &Touch{
						zone: ZoneUndecided,
					}
					if len(touches) > 1 {
						for _, t := range touches {
							t.zone = ZoneNone
						}
					}
				}

			case evdev.ABS_MT_POSITION_X:
				t, ok := touches[slot]
				if !ok {
					break
				}
				if !t.hasX0 {
					t.x0 = ev.Value
					t.hasX0 = true
				} else if absInt32(ev.Value-t.x0) > maxTapMove {
					tapMoved = true
				}
				decideZone(t, maxX, maxY)
				if t.zone == ZoneTop && t.hasLast {
					t.acc += float64(ev.Value) - t.last
					t.last = float64(ev.Value)
					for math.Abs(t.acc) >= stepH {
						if t.acc > 0 {
							spawn(cmdTopEdgeRight)
							t.acc -= stepH
						} else {
							spawn(cmdTopEdgeLeft)
							t.acc += stepH
						}
					}
				}

			case evdev.ABS_MT_POSITION_Y:
				t, ok := touches[slot]
				if !ok {
					break
				}
				if !t.hasY0 {
					t.y0 = ev.Value
					t.hasY0 = true
				} else if absInt32(ev.Value-t.y0) > maxTapMove {
					tapMoved = true
				}
				decideZone(t, maxX, maxY)
				if (t.zone == ZoneLeft || t.zone == ZoneRight) && t.hasLast {
					t.acc += t.last - float64(ev.Value)
					t.last = float64(ev.Value)
					for math.Abs(t.acc) >= step {
						if t.zone == ZoneLeft {
							if t.acc > 0 {
								spawn(cmdLeftEdgeUp)
								t.acc -= step
							} else {
								spawn(cmdLeftEdgeDown)
								t.acc += step
							}
						} else {
							if t.acc > 0 {
								spawn(cmdRightEdgeUp)
								t.acc -= step
							} else {
								spawn(cmdRightEdgeDown)
								t.acc += step
							}
						}
					}
				}
			}

		case evdev.EV_KEY:
			now := float64(ev.Time.Sec) + float64(ev.Time.Usec)/1e6
			if ev.Code == evdev.BTN_TOUCH {
				if ev.Value != 0 {
					tapStart = now
					hasTap = true
					tapPeak = 0
					tapMoved = false
				} else if hasTap {
					if (now-tapStart) < maxTapTime && !tapMoved {
						if cmd, ok := taps[tapPeak]; ok {
							spawn(cmd)
						}
					}
					hasTap = false
				}
			} else if ev.Value != 0 && hasTap {
				if count, ok := fingerCodes[ev.Code]; ok {
					if count > tapPeak {
						tapPeak = count
					}
				}
			}
		}
	}
}
