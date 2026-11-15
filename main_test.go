//go:build linux

package main

import (
	"math"
	"testing"
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


