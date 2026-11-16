//go:build linux

package main

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestDecideZone(t *testing.T) {
	maxX := 3200.0
	maxY := 1800.0

	tests := []struct {
		name         string
		x0           int32
		y0           int32
		expectedZone Zone
		expectedLast float64
	}{
		{
			name:         "right edge",
			x0:           3100, // > 3200 * 0.94 = 3008
			y0:           500,
			expectedZone: ZoneRight,
			expectedLast: 500,
		},
		{
			name:         "left edge",
			x0:           100, // < 3200 * 0.06 = 192
			y0:           500,
			expectedZone: ZoneLeft,
			expectedLast: 500,
		},
		{
			name:         "top edge",
			x0:           1500,
			y0:           100, // < 1800 * 0.08 = 144
			expectedZone: ZoneTop,
			expectedLast: 1500,
		},
		{
			name:         "center zone none",
			x0:           1500,
			y0:           900,
			expectedZone: ZoneNone,
			expectedLast: 900,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			touch := &Touch{
				zone:  ZoneUndecided,
				x0:    tt.x0,
				hasX0: true,
				y0:    tt.y0,
				hasY0: true,
			}
			decideZone(touch, maxX, maxY)
			if touch.zone != tt.expectedZone {
				t.Fatalf("expected zone %v, got %v", tt.expectedZone, touch.zone)
			}
			if !touch.hasLast || touch.last != tt.expectedLast {
				t.Fatalf("expected last %v, got %v", tt.expectedLast, touch.last)
			}
		})
	}
}

func TestDecideZoneIncomplete(t *testing.T) {
	maxX := 3200.0
	maxY := 1800.0

	touch := &Touch{
		zone:  ZoneUndecided,
		x0:    100,
		hasX0: true,
		hasY0: false,
	}
	decideZone(touch, maxX, maxY)
	if touch.zone != ZoneUndecided {
		t.Fatalf("expected undecided zone when y0 is missing")
	}

	touch2 := &Touch{
		zone:  ZoneUndecided,
		y0:    100,
		hasX0: false,
		hasY0: true,
	}
	decideZone(touch2, maxX, maxY)
	if touch2.zone != ZoneUndecided {
		t.Fatalf("expected undecided zone when x0 is missing")
	}
}

func TestAccumulator(t *testing.T) {
	step := 50.0

	// Test positive accumulator (upward movement)
	acc := 125.0
	steps := 0
	for math.Abs(acc) >= step {
		if acc > 0 {
			steps++
			acc -= step
		} else {
			steps--
			acc += step
		}
	}
	if steps != 2 {
		t.Fatalf("expected 2 steps, got %d", steps)
	}
	if acc != 25.0 {
		t.Fatalf("expected remaining acc 25.0, got %f", acc)
	}

	// Test negative accumulator (downward movement)
	acc = -125.0
	steps = 0
	for math.Abs(acc) >= step {
		if acc > 0 {
			steps++
			acc -= step
		} else {
			steps--
			acc += step
		}
	}
	if steps != -2 {
		t.Fatalf("expected -2 steps, got %d", steps)
	}
	if acc != -25.0 {
		t.Fatalf("expected remaining acc -25.0, got %f", acc)
	}
}

func TestTapConditions(t *testing.T) {
	// Good 4-finger tap
	start := 10.0
	now := 10.15
	tapMoved := false
	tapPeak := 4

	if !((now-start) < maxTapTime && !tapMoved && (tapPeak == 4 || tapPeak == 5)) {
		t.Fatalf("tap should have passed")
	}

	// Moved tap
	tapMoved = true
	if (now-start) < maxTapTime && !tapMoved && (tapPeak == 4 || tapPeak == 5) {
		t.Fatalf("moved tap should be suppressed")
	}

	// Slow tap (> 0.25s)
	tapMoved = false
	now = 10.30
	if (now-start) < maxTapTime && !tapMoved && (tapPeak == 4 || tapPeak == 5) {
		t.Fatalf("slow tap should be suppressed")
	}

	// 3-finger tap
	now = 10.10
	tapPeak = 3
	if (now-start) < maxTapTime && !tapMoved && (tapPeak == 4 || tapPeak == 5) {
		t.Fatalf("3-finger tap should be suppressed")
	}
}

func TestEdgeTapConditions(t *testing.T) {
	evalEdgeTap := func(now, tapStart, curMaxTapTime float64, tapMoved, sliderFired bool, tapPeak, maxActiveTouches int, zone Zone) bool {
		return (now-tapStart) < curMaxTapTime && !tapMoved && !sliderFired && (tapPeak == 1 || tapPeak == 0) && maxActiveTouches <= 1 && (zone == ZoneLeft || zone == ZoneRight || zone == ZoneTop)
	}

	start := 10.0
	curMaxTapTime := 0.25

	// Valid 1-finger left edge tap
	if !evalEdgeTap(10.15, start, curMaxTapTime, false, false, 1, 1, ZoneLeft) {
		t.Fatalf("expected left edge tap to pass")
	}

	// Valid 1-finger right edge tap
	if !evalEdgeTap(10.15, start, curMaxTapTime, false, false, 1, 1, ZoneRight) {
		t.Fatalf("expected right edge tap to pass")
	}

	// Valid 1-finger top edge tap
	if !evalEdgeTap(10.15, start, curMaxTapTime, false, false, 1, 1, ZoneTop) {
		t.Fatalf("expected top edge tap to pass")
	}

	// Zone None (center of touchpad) -> suppressed
	if evalEdgeTap(10.15, start, curMaxTapTime, false, false, 1, 1, ZoneNone) {
		t.Fatalf("center zone should be suppressed")
	}

	// Zone Undecided -> suppressed
	if evalEdgeTap(10.15, start, curMaxTapTime, false, false, 1, 1, ZoneUndecided) {
		t.Fatalf("undecided zone should be suppressed")
	}

	// Tap moved -> suppressed
	if evalEdgeTap(10.15, start, curMaxTapTime, true, false, 1, 1, ZoneLeft) {
		t.Fatalf("moved tap should be suppressed")
	}

	// Slider fired -> suppressed
	if evalEdgeTap(10.15, start, curMaxTapTime, false, true, 1, 1, ZoneLeft) {
		t.Fatalf("slider fired tap should be suppressed")
	}

	// Duration exceeded -> suppressed
	if evalEdgeTap(10.30, start, curMaxTapTime, false, false, 1, 1, ZoneLeft) {
		t.Fatalf("slow tap should be suppressed")
	}

	// Multi-finger by peak -> suppressed
	if evalEdgeTap(10.15, start, curMaxTapTime, false, false, 2, 1, ZoneLeft) {
		t.Fatalf("multi-finger peak should be suppressed")
	}

	// Multi-finger by active touches -> suppressed
	if evalEdgeTap(10.15, start, curMaxTapTime, false, false, 1, 2, ZoneLeft) {
		t.Fatalf("multi active touches should be suppressed")
	}
}

func TestParseCmd(t *testing.T) {
	tests := []struct {
		input    string
		expected []string
	}{
		{
			input:    `notify-send "4 fingers"`,
			expected: []string{"notify-send", "4 fingers"},
		},
		{
			input:    `notify-send '4-finger tap'`,
			expected: []string{"notify-send", "4-finger tap"},
		},
		{
			input:    `wpctl set-volume -l 1.0 @DEFAULT_AUDIO_SINK@ 2%+`,
			expected: []string{"wpctl", "set-volume", "-l", "1.0", "@DEFAULT_AUDIO_SINK@", "2%+"},
		},
		{
			input:    `echo "hello \"world\""`,
			expected: []string{"echo", `hello "world"`},
		},
		{
			input:    "",
			expected: nil,
		},
		{
			input:    "   ",
			expected: nil,
		},
	}

	for _, tt := range tests {
		got := parseCmd(tt.input)
		if len(got) != len(tt.expected) {
			t.Fatalf("for %q, expected %v (len %d), got %v (len %d)", tt.input, tt.expected, len(tt.expected), got, len(got))
		}
		for i := range got {
			if got[i] != tt.expected[i] {
				t.Fatalf("for %q at index %d: expected %q, got %q", tt.input, i, tt.expected[i], got[i])
			}
		}
	}
}

func TestGetLockPath(t *testing.T) {
	tempDir := t.TempDir()

	// With XDG_RUNTIME_DIR set
	t.Setenv("XDG_RUNTIME_DIR", tempDir)
	lockPath := getLockPath("/custom/config")
	expected := tempDir + "/touchzones.lock"
	if lockPath != expected {
		t.Fatalf("expected %q, got %q", expected, lockPath)
	}

	// Without XDG_RUNTIME_DIR
	t.Setenv("XDG_RUNTIME_DIR", "")
	customConfig := tempDir + "/cfg"
	lockPathFallback := getLockPath(customConfig)
	expectedFallback := customConfig + "/touchzones.lock"
	if lockPathFallback != expectedFallback {
		t.Fatalf("expected %q, got %q", expectedFallback, lockPathFallback)
	}
}

func TestGetRunningPIDNotRunning(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tempDir)

	_, err := getRunningPID(tempDir)
	if err == nil {
		t.Fatalf("expected error when no lock file exists, got nil")
	}
}

func TestKillRunningDaemonNotRunning(t *testing.T) {
	tempDir := t.TempDir()
	t.Setenv("XDG_RUNTIME_DIR", tempDir)

	err := killDaemon(tempDir)
	if err == nil {
		t.Fatalf("expected error when no lock file exists, got nil")
	}
}

func TestLoadConfigEdgeTaps(t *testing.T) {
	tempDir := t.TempDir()
	cfgPath := filepath.Join(tempDir, "touchzones.conf")
	content := `
max_double_tap_time = 0.20
left_tap = notify-send "left tap"
left_double_tap = notify-send "left double tap"
right_tap = wpctl set-mute @DEFAULT_AUDIO_SINK@ toggle
right_double_tap = notify-send "right double tap"
top_tap = notify-send "top tap"
top_double_tap = notify-send "top double tap"
`
	if err := os.WriteFile(cfgPath, []byte(content), 0644); err != nil {
		t.Fatalf("failed to write test config: %v", err)
	}

	if err := loadConfig(cfgPath); err != nil {
		t.Fatalf("loadConfig failed: %v", err)
	}

	configMu.RLock()
	defer configMu.RUnlock()

	if maxDoubleTapTime != 0.20 {
		t.Errorf("expected maxDoubleTapTime 0.20, got %f", maxDoubleTapTime)
	}
	if !reflect.DeepEqual(cmdLeftTap, []string{"notify-send", "left tap"}) {
		t.Errorf("unexpected cmdLeftTap: %v", cmdLeftTap)
	}
	if !reflect.DeepEqual(cmdLeftDoubleTap, []string{"notify-send", "left double tap"}) {
		t.Errorf("unexpected cmdLeftDoubleTap: %v", cmdLeftDoubleTap)
	}
	if !reflect.DeepEqual(cmdRightTap, []string{"wpctl", "set-mute", "@DEFAULT_AUDIO_SINK@", "toggle"}) {
		t.Errorf("unexpected cmdRightTap: %v", cmdRightTap)
	}
	if !reflect.DeepEqual(cmdRightDoubleTap, []string{"notify-send", "right double tap"}) {
		t.Errorf("unexpected cmdRightDoubleTap: %v", cmdRightDoubleTap)
	}
	if !reflect.DeepEqual(cmdTopTap, []string{"notify-send", "top tap"}) {
		t.Errorf("unexpected cmdTopTap: %v", cmdTopTap)
	}
	if !reflect.DeepEqual(cmdTopDoubleTap, []string{"notify-send", "top double tap"}) {
		t.Errorf("unexpected cmdTopDoubleTap: %v", cmdTopDoubleTap)
	}
}

func TestEdgeTapDetectorImmediate(t *testing.T) {
	configMu.Lock()
	cmdLeftTap = []string{"left_single"}
	cmdLeftDoubleTap = nil
	configMu.Unlock()

	ch := make(chan []string, 10)
	detector := newEdgeTapDetector(func(args []string) {
		ch <- args
	})
	defer detector.Reset()

	detector.HandleTap(ZoneLeft, 10.0)

	select {
	case cmd := <-ch:
		if !reflect.DeepEqual(cmd, []string{"left_single"}) {
			t.Fatalf("expected left_single, got %v", cmd)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("timed out waiting for immediate single tap")
	}
}

func TestEdgeTapDetectorDoubleTap(t *testing.T) {
	configMu.Lock()
	cmdLeftTap = []string{"left_single"}
	cmdLeftDoubleTap = []string{"left_double"}
	maxDoubleTapTime = 0.15
	configMu.Unlock()

	ch := make(chan []string, 10)
	detector := newEdgeTapDetector(func(args []string) {
		ch <- args
	})
	defer detector.Reset()

	// First tap at t=10.0
	detector.HandleTap(ZoneLeft, 10.0)

	// Second tap at t=10.05 (within 0.15s window)
	detector.HandleTap(ZoneLeft, 10.05)

	select {
	case cmd := <-ch:
		if !reflect.DeepEqual(cmd, []string{"left_double"}) {
			t.Fatalf("expected left_double, got %v", cmd)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for double tap")
	}

	// Ensure single tap never arrives
	select {
	case cmd := <-ch:
		t.Fatalf("unexpected command arrived: %v", cmd)
	case <-time.After(200 * time.Millisecond):
		// Success: single tap was cancelled
	}
}

func TestEdgeTapDetectorSingleTapDelayed(t *testing.T) {
	configMu.Lock()
	cmdRightTap = []string{"right_single"}
	cmdRightDoubleTap = []string{"right_double"}
	maxDoubleTapTime = 0.05
	configMu.Unlock()

	ch := make(chan []string, 10)
	detector := newEdgeTapDetector(func(args []string) {
		ch <- args
	})
	defer detector.Reset()

	detector.HandleTap(ZoneRight, 10.0)

	// It should NOT arrive immediately
	select {
	case cmd := <-ch:
		t.Fatalf("command fired too early: %v", cmd)
	case <-time.After(20 * time.Millisecond):
	}

	// It should arrive after the timeout
	select {
	case cmd := <-ch:
		if !reflect.DeepEqual(cmd, []string{"right_single"}) {
			t.Fatalf("expected right_single, got %v", cmd)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for delayed single tap")
	}
}

func TestEdgeTapDetectorZoneSwitch(t *testing.T) {
	configMu.Lock()
	cmdLeftTap = []string{"left_single"}
	cmdLeftDoubleTap = []string{"left_double"}
	cmdRightTap = []string{"right_single"}
	cmdRightDoubleTap = nil
	maxDoubleTapTime = 0.20
	configMu.Unlock()

	ch := make(chan []string, 10)
	detector := newEdgeTapDetector(func(args []string) {
		ch <- args
	})
	defer detector.Reset()

	// Tap left
	detector.HandleTap(ZoneLeft, 10.0)

	// Tap right immediately
	detector.HandleTap(ZoneRight, 10.02)

	// Both left_single (flushed) and right_single (immediate because no right_double_tap) should arrive
	received := make([][]string, 0, 2)
	for i := 0; i < 2; i++ {
		select {
		case cmd := <-ch:
			received = append(received, cmd)
		case <-time.After(100 * time.Millisecond):
			t.Fatalf("timed out waiting for commands, got %v", received)
		}
	}

	if !reflect.DeepEqual(received[0], []string{"left_single"}) {
		t.Errorf("expected first cmd left_single, got %v", received[0])
	}
	if !reflect.DeepEqual(received[1], []string{"right_single"}) {
		t.Errorf("expected second cmd right_single, got %v", received[1])
	}
}

func TestEdgeTapDetectorFlushPending(t *testing.T) {
	configMu.Lock()
	cmdTopTap = []string{"top_single"}
	cmdTopDoubleTap = []string{"top_double"}
	maxDoubleTapTime = 0.20
	configMu.Unlock()

	ch := make(chan []string, 10)
	detector := newEdgeTapDetector(func(args []string) {
		ch <- args
	})
	defer detector.Reset()

	detector.HandleTap(ZoneTop, 10.0)
	detector.FlushPending()

	select {
	case cmd := <-ch:
		if !reflect.DeepEqual(cmd, []string{"top_single"}) {
			t.Fatalf("expected top_single, got %v", cmd)
		}
	case <-time.After(50 * time.Millisecond):
		t.Fatal("timed out waiting for flushed single tap")
	}
}

func TestEdgeTapDetectorSameZoneExpired(t *testing.T) {
	configMu.Lock()
	cmdLeftTap = []string{"left_single"}
	cmdLeftDoubleTap = []string{"left_double"}
	maxDoubleTapTime = 0.15
	configMu.Unlock()

	ch := make(chan []string, 10)
	detector := newEdgeTapDetector(func(args []string) {
		ch <- args
	})
	defer detector.Reset()

	// Tap 1 at t=10.0
	detector.HandleTap(ZoneLeft, 10.0)

	// Tap 2 at t=10.25 (exceeds 0.15s window) -> should flush Tap 1 as single tap
	detector.HandleTap(ZoneLeft, 10.25)

	select {
	case cmd := <-ch:
		if !reflect.DeepEqual(cmd, []string{"left_single"}) {
			t.Fatalf("expected flushed left_single, got %v", cmd)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for flushed single tap")
	}

	// Tap 3 at t=10.30 (50ms after Tap 2) -> should register double tap with Tap 2
	detector.HandleTap(ZoneLeft, 10.30)

	select {
	case cmd := <-ch:
		if !reflect.DeepEqual(cmd, []string{"left_double"}) {
			t.Fatalf("expected left_double, got %v", cmd)
		}
	case <-time.After(100 * time.Millisecond):
		t.Fatal("timed out waiting for double tap")
	}
}




