//go:build linux

package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func getLockPath(configDir string) string {
	r := os.Getenv("XDG_RUNTIME_DIR")
	if r != "" {
		return path.Join(r, "touchzones.lock")
	}
	_ = os.MkdirAll(configDir, 0755)
	return path.Join(configDir, "touchzones.lock")
}

func getRunningPID(configDir string) (int, error) {
	lockPath := getLockPath(configDir)
	file, err := os.Open(lockPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, fmt.Errorf("touchzones is not running")
		}
		return 0, fmt.Errorf("could not read lock file: %w", err)
	}
	defer file.Close()

	// If we can acquire the lock, no daemon is running
	err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err == nil {
		_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
		return 0, fmt.Errorf("touchzones is not running")
	}

	pidB, err := os.ReadFile(lockPath)
	if err != nil {
		return 0, fmt.Errorf("could not read PID from lock file: %w", err)
	}
	pidStr := strings.TrimSpace(string(pidB))
	pid, err := strconv.Atoi(pidStr)
	if err != nil || pid <= 0 {
		return 0, fmt.Errorf("invalid PID in lock file: %q", pidStr)
	}

	// Verify that the target PID is actually a touchzones executable
	target, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return 0, fmt.Errorf("could not verify process binary for PID %d: %w", pid, err)
	}

	self, err := os.Executable()
	selfBase := "touchzones"
	if err == nil {
		selfBase = strings.TrimSuffix(path.Base(self), " (deleted)")
	}

	targetBase := strings.TrimSuffix(path.Base(target), " (deleted)")
	if targetBase != "touchzones" && targetBase != selfBase {
		return 0, fmt.Errorf("security error: PID %d is %q, not a touchzones process", pid, target)
	}

	return pid, nil
}

func killDaemon(configDir string) error {
	pid, err := getRunningPID(configDir)
	if err != nil {
		return err
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM to PID %d: %w", pid, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if err := syscall.Kill(pid, 0); err != nil {
			fmt.Printf("Terminated touchzones daemon (PID %d)\n", pid)
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}

	_ = syscall.Kill(pid, syscall.SIGKILL)
	fmt.Printf("Force-killed touchzones daemon (PID %d)\n", pid)
	return nil
}

func acquireLock(configDir string) (*os.File, error) {
	lockPath := getLockPath(configDir)

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
