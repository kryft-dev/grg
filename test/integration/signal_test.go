package integration

import (
	"fmt"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// setupWorkloadRepo creates a repository with enough commit history to test process interruption.
func setupWorkloadRepo(t *testing.T) *TestRepo {
	t.Helper()
	repo := NewTestRepo(t)

	for i := 1; i <= 25; i++ {
		files := make(map[string]string)
		for j := 1; j <= 10; j++ {
			fileName := fmt.Sprintf("pkg%d/file%d.txt", i, j)
			files[fileName] = fmt.Sprintf("Commit %d File %d line 1\nCommit %d File %d line 2\nTOKEN_WORKLOAD_MARKER\n", i, j, i, j)
		}
		repo.Commit(fmt.Sprintf("Commit batch %d", i), files)
	}

	return repo
}

// TestSignal_SIGINT_Cancellation verifies that sending SIGINT to grg terminates execution promptly.
func TestSignal_SIGINT_Cancellation(t *testing.T) {
	repo := setupWorkloadRepo(t)
	bin := getGRGBinary(t)

	cmd := exec.Command(bin, "--color=never", "TOKEN_WORKLOAD_MARKER")
	cmd.Dir = repo.Dir

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed starting grg subprocess: %v", err)
	}

	// Allow subprocess a moment to begin traversing commit DAG
	time.Sleep(10 * time.Millisecond)

	// Send SIGINT
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Logf("signal error (process may have already finished): %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("grg subprocess failed to terminate within 3 seconds after SIGINT")
	case err := <-done:
		// Process terminated promptly
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					if ws.Signaled() && ws.Signal() == syscall.SIGINT {
						// Terminated by SIGINT directly (status 130 or -1 in Go)
						t.Logf("subprocess terminated directly by SIGINT signal")
						return
					}
				}
			}
		}

		// Valid termination exit codes:
		// 2: handled cleanly by signal.NotifyContext
		// 130: standard bash status for 128 + SIGINT
		// -1: process killed by signal on Unix
		// 0: if search finished before signal arrived
		if exitCode != 2 && exitCode != 130 && exitCode != -1 && exitCode != 0 {
			t.Errorf("expected exit code 2, 130, or -1 on SIGINT, got %d (err: %v)", exitCode, err)
		}
	}
}

// TestSignal_SIGTERM_Cancellation verifies that sending SIGTERM terminates execution promptly.
func TestSignal_SIGTERM_Cancellation(t *testing.T) {
	repo := setupWorkloadRepo(t)
	bin := getGRGBinary(t)

	cmd := exec.Command(bin, "--color=never", "TOKEN_WORKLOAD_MARKER")
	cmd.Dir = repo.Dir

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed starting grg subprocess: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	// Send SIGTERM
	if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
		t.Logf("signal error: %v", err)
	}

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("grg subprocess failed to terminate within 3 seconds after SIGTERM")
	case err := <-done:
		exitCode := 0
		if err != nil {
			if exitErr, ok := err.(*exec.ExitError); ok {
				exitCode = exitErr.ExitCode()
				if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok {
					if ws.Signaled() && ws.Signal() == syscall.SIGTERM {
						t.Logf("subprocess terminated directly by SIGTERM signal")
						return
					}
				}
			}
		}

		// Valid termination codes: 2 (handled), 143 (128 + 15), -1 (signaled), 0 (completed)
		if exitCode != 2 && exitCode != 143 && exitCode != -1 && exitCode != 0 {
			t.Errorf("expected exit code 2, 143, or -1 on SIGTERM, got %d (err: %v)", exitCode, err)
		}
	}
}

// TestSignal_ImmediateInterrupt verifies sending signal immediately upon startup does not hang.
func TestSignal_ImmediateInterrupt(t *testing.T) {
	repo := setupWorkloadRepo(t)
	bin := getGRGBinary(t)

	cmd := exec.Command(bin, "--color=never", "TOKEN_WORKLOAD_MARKER")
	cmd.Dir = repo.Dir

	if err := cmd.Start(); err != nil {
		t.Fatalf("failed starting grg subprocess: %v", err)
	}

	// Immediately send interrupt without sleeping
	_ = cmd.Process.Signal(syscall.SIGINT)

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	select {
	case <-time.After(3 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("grg subprocess hung after immediate SIGINT")
	case <-done:
		// Succeeded in exiting within timeout
	}
}
