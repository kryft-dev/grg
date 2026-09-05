package integration

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

// setupWorkloadRepo creates a repository with enough commit history to test process interruption.
//
// Note that this workload is weak on purpose-preserving history: 25 commits of
// 10 tiny files are searched in a few milliseconds, so the three tests below
// almost certainly finish before their signal is delivered, and all of them
// accept exit code 0 as valid. They only prove that a signal does not wedge
// the process. TestSignal_SecondSIGINT_ForceExits carries the real assertion:
// it pins the process in a state where the signal has to do the work, and
// accepts nothing but a non-zero exit.
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

// setupOutputHeavyRepo creates a repository whose search emits far more output
// than a pipe buffer holds (64 KiB on Linux), so a grg process whose stdout is
// never drained is guaranteed to fill the pipe and block in write(2) with
// matches still pending.
func setupOutputHeavyRepo(t *testing.T) *TestRepo {
	t.Helper()
	repo := NewTestRepo(t)

	for i := 1; i <= 30; i++ {
		files := make(map[string]string)
		for j := 1; j <= 12; j++ {
			var b strings.Builder
			for k := 1; k <= 8; k++ {
				fmt.Fprintf(&b, "commit %d file %d line %d TOKEN_WORKLOAD_MARKER padding padding padding padding\n", i, j, k)
			}
			files[fmt.Sprintf("pkg%d/file%d.txt", j, j)] = b.String()
		}
		repo.Commit(fmt.Sprintf("Commit batch %d", i), files)
	}

	return repo
}

// TestSignal_SecondSIGINT_ForceExits verifies that a second SIGINT terminates
// grg even when the run cannot unwind on its own.
//
// The process is pinned in an uninterruptible write: its stdout is a pipe that
// nobody reads, so once the pipe buffer is full every remaining match line
// blocks in write(2). No context check can rescue that goroutine, which is
// exactly the situation a user hits when grg is piped into a stalled consumer.
// The first SIGINT is therefore necessarily absorbed by the installed handler
// and changes nothing observable. Only releasing that handler once cancellation
// has fired - restoring the default disposition - lets the second SIGINT end
// the process. Without that release, package signal keeps relaying and then
// silently dropping every later signal and the process survives until SIGKILL.
func TestSignal_SecondSIGINT_ForceExits(t *testing.T) {
	repo := setupOutputHeavyRepo(t)
	bin := getGRGBinary(t)

	// The parent closes its copy of the write end so the child owns the only
	// writer, and holds the read end open without ever reading it so the child
	// blocks instead of receiving EPIPE.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatalf("failed creating pipe: %v", err)
	}
	defer pr.Close()

	cmd := exec.Command(bin, "--color=never", "TOKEN_WORKLOAD_MARKER")
	cmd.Dir = repo.Dir
	cmd.Stdout = pw

	if err := cmd.Start(); err != nil {
		_ = pw.Close()
		t.Fatalf("failed starting grg subprocess: %v", err)
	}
	_ = pw.Close()

	done := make(chan error, 1)
	go func() {
		done <- cmd.Wait()
	}()

	// Premise check: the search itself is quick, but the output cannot drain, so
	// the process must still be alive and stuck. Had it exited, the workload
	// would fit in the pipe buffer and the rest of this test would prove nothing.
	select {
	case err := <-done:
		t.Fatalf("grg exited (%v) before filling the stdout pipe; workload is too small for this test to mean anything", err)
	case <-time.After(1 * time.Second):
	}

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("failed sending first SIGINT: %v", err)
	}

	// Give cancellation time to propagate and release the signal handler.
	select {
	case err := <-done:
		// Not expected while blocked in write, but a prompt non-zero exit is a
		// perfectly good outcome for an interrupted run.
		assertInterruptedExit(t, err)
		return
	case <-time.After(250 * time.Millisecond):
	}

	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatalf("failed sending second SIGINT: %v", err)
	}

	select {
	case <-time.After(2 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatal("second SIGINT was swallowed: grg was still alive 2s later and only SIGKILL ended it")
	case err := <-done:
		assertInterruptedExit(t, err)
	}
}

// assertInterruptedExit requires that grg died because it was interrupted:
// killed outright by SIGINT once the default disposition was restored, or
// exited non-zero after handling the cancellation itself. Exit code 0 would
// claim the search completed successfully, which it did not.
func assertInterruptedExit(t *testing.T, err error) {
	t.Helper()

	if err == nil {
		t.Fatal("grg exited 0 after being interrupted; an interrupted run must not report success")
	}

	exitErr, ok := err.(*exec.ExitError)
	if !ok {
		t.Fatalf("waiting on grg failed: %v", err)
	}

	if ws, ok := exitErr.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
		if ws.Signal() != syscall.SIGINT {
			t.Fatalf("expected termination by SIGINT, got %v", ws.Signal())
		}
		return
	}

	if code := exitErr.ExitCode(); code == 0 {
		t.Fatalf("expected a non-zero exit code after interruption, got %d", code)
	}
}
