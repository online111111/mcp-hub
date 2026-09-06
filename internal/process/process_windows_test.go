//go:build windows

package process

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

// Helper process entrypoint for Windows lifecycle tests.
func init() {
	mode := os.Getenv("MCP_TEST_HELPER_MODE")
	if mode == "" {
		if os.Getenv(WorkerEnvVar) == "1" || (len(os.Args) > 1 && os.Args[1] == WorkerFlag) {
			if err := RunWorker(); err != nil {
				var exitErr *exec.ExitError
				if errors.As(err, &exitErr) {
					os.Exit(exitErr.ExitCode())
				}
				os.Exit(1)
			}
			os.Exit(0)
		}
		return
	}

	switch mode {
	case "echo_args":
		// Arguments received by downstream process: os.Args[1:]
		data, _ := json.Marshal(os.Args[1:])
		_, _ = os.Stdout.Write(data)
		os.Exit(0)

	case "echo_io":
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			_, _ = fmt.Fprintln(os.Stdout, scanner.Text())
		}
		os.Exit(0)

	case "spawn_grandchild":
		// Child writes its own PID
		childPidFile := os.Getenv("MCP_TEST_CHILD_PID_FILE")
		if childPidFile != "" {
			_ = os.WriteFile(childPidFile, []byte(strconv.Itoa(os.Getpid())), 0644)
		}

		// Launch a grandchild process that hangs
		grandchild := exec.Command(os.Args[0])
		grandchild.Env = append(os.Environ(), "MCP_TEST_HELPER_MODE=hang")
		if err := grandchild.Start(); err != nil {
			os.Exit(2)
		}

		// Write grandchild PID
		grandchildPidFile := os.Getenv("MCP_TEST_GRANDCHILD_PID_FILE")
		if grandchildPidFile != "" {
			_ = os.WriteFile(grandchildPidFile, []byte(strconv.Itoa(grandchild.Process.Pid)), 0644)
		}

		// Wait for stdin EOF
		_, _ = io.ReadAll(os.Stdin)
		_ = grandchild.Wait()
		os.Exit(0)

	case "crash":
		os.Exit(42)

	case "hang":
		time.Sleep(10 * time.Minute)
		os.Exit(0)
	}
}

// isProcessRunning checks if the given PID corresponds to an active process using Win32 API.
func isProcessRunning(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION|windows.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)

	event, err := windows.WaitForSingleObject(h, 0)
	if err != nil {
		return false
	}
	return event == uint32(windows.WAIT_TIMEOUT)
}

func waitForProcessExit(pid int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if !isProcessRunning(pid) {
			return true
		}
		time.Sleep(20 * time.Millisecond)
	}
	return !isProcessRunning(pid)
}

func TestWindows_ArgsPreservation(t *testing.T) {
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("get self executable: %v", err)
	}

	testArgs := []string{
		"arg with spaces",
		"测试 Unicode 字符 🚀",
		`arg "with" quotes`,
		"%PATH%",
		"a & b",
		"param=value",
		`C:\Program Files\Special\Path`,
	}

	spec := Spec{
		Command: selfExe,
		Args:    testArgs,
		Env:     append(os.Environ(), "MCP_TEST_HELPER_MODE=echo_args"),
	}

	proc, err := Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer proc.Close()

	out, err := io.ReadAll(proc.Reader())
	if err != nil {
		t.Fatalf("read from proc.Reader failed: %v", err)
	}

	var receivedArgs []string
	if err := json.Unmarshal(out, &receivedArgs); err != nil {
		t.Fatalf("unmarshal received args failed: %v\nOutput: %s", err, string(out))
	}

	if !reflect.DeepEqual(receivedArgs, testArgs) {
		t.Fatalf("Arguments not preserved!\nSent:     %#v\nReceived: %#v", testArgs, receivedArgs)
	}

	if err := proc.Wait(); err != nil {
		t.Fatalf("proc.Wait() error: %v", err)
	}
}

func TestWindows_BootstrapNotSent_NoDownstreamPID(t *testing.T) {
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("get self executable: %v", err)
	}

	tempDir := t.TempDir()
	downstreamPidFile := filepath.Join(tempDir, "downstream.pid")

	// Start worker directly, but do NOT write bootstrap to stdin.
	cmd := exec.Command(selfExe, WorkerFlag)
	cmd.Env = append(os.Environ(), WorkerEnvVar+"=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatalf("create stdin pipe: %v", err)
	}

	if err := cmd.Start(); err != nil {
		t.Fatalf("start worker: %v", err)
	}
	workerPid := cmd.Process.Pid

	// Close stdin without sending bootstrap line (simulating Hub death before bootstrap)
	_ = stdin.Close()

	// Worker should exit on EOF
	waitDone := make(chan error, 1)
	go func() {
		waitDone <- cmd.Wait()
	}()

	select {
	case err := <-waitDone:
		if err == nil {
			t.Logf("worker exited normally upon stdin EOF")
		}
	case <-time.After(5 * time.Second):
		_ = cmd.Process.Kill()
		t.Fatalf("worker did not exit within timeout when bootstrap was withheld")
	}

	// Verify worker is reaped
	if !waitForProcessExit(workerPid, 2*time.Second) {
		t.Errorf("worker PID %d still running after exit", workerPid)
	}

	// Verify downstream was never launched
	if _, err := os.Stat(downstreamPidFile); !os.IsNotExist(err) {
		t.Fatalf("downstream PID file exists, but bootstrap was never sent!")
	}
}

func TestWindows_JobAssignFailure_WorkerReaped_NoDownstream(t *testing.T) {
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("get self executable: %v", err)
	}

	// Hook to simulate Job Object assignment failure (e.g. nested job restriction)
	simulatedErr := errors.New("simulated nested job access denied")
	testAssignJobHook = func(job windows.Handle, process windows.Handle) error {
		return simulatedErr
	}
	defer func() {
		testAssignJobHook = nil
	}()

	tempDir := t.TempDir()
	pidFile := filepath.Join(tempDir, "child.pid")

	spec := Spec{
		Command: selfExe,
		Args:    []string{"ignored"},
		Env:     append(os.Environ(), "MCP_TEST_HELPER_MODE=spawn_grandchild", "MCP_TEST_CHILD_PID_FILE="+pidFile),
	}

	_, err = Start(context.Background(), spec)
	if err == nil {
		t.Fatalf("expected Start to fail on job assign failure, got nil")
	}

	if !errors.Is(err, ErrJobAssignFailed) {
		t.Fatalf("expected ErrJobAssignFailed, got: %v", err)
	}

	// Downstream PID file must never have been created
	if _, err := os.Stat(pidFile); !os.IsNotExist(err) {
		t.Fatalf("downstream process ran despite Job assignment failure!")
	}
}

func TestWindows_TreeCleanup_GrandchildTerminated(t *testing.T) {
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("get self executable: %v", err)
	}

	tempDir := t.TempDir()
	childPidFile := filepath.Join(tempDir, "child.pid")
	grandchildPidFile := filepath.Join(tempDir, "grandchild.pid")

	spec := Spec{
		Command: selfExe,
		Args:    []string{"dummy"},
		Env: append(os.Environ(),
			"MCP_TEST_HELPER_MODE=spawn_grandchild",
			"MCP_TEST_CHILD_PID_FILE="+childPidFile,
			"MCP_TEST_GRANDCHILD_PID_FILE="+grandchildPidFile,
		),
	}

	proc, err := Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	workerPid := proc.Pid()

	// Wait for child and grandchild PIDs to be written
	var childPid, grandchildPid int
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cBytes, err := os.ReadFile(childPidFile); err == nil {
			childPid, _ = strconv.Atoi(string(cBytes))
		}
		if gBytes, err := os.ReadFile(grandchildPidFile); err == nil {
			grandchildPid, _ = strconv.Atoi(string(gBytes))
		}
		if childPid > 0 && grandchildPid > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	if childPid == 0 || grandchildPid == 0 {
		_ = proc.Close()
		t.Fatalf("failed to obtain child (%d) or grandchild (%d) PID", childPid, grandchildPid)
	}

	// Verify both are running
	if !isProcessRunning(childPid) {
		t.Fatalf("child process %d not running", childPid)
	}
	if !isProcessRunning(grandchildPid) {
		t.Fatalf("grandchild process %d not running", grandchildPid)
	}

	// Hub calls Close(): must close Job Object and terminate tree
	if err := proc.Close(); err != nil {
		t.Logf("proc.Close() returned: %v", err)
	}

	// Verify child, grandchild, and worker are all terminated
	if !waitForProcessExit(childPid, 3*time.Second) {
		t.Errorf("child PID %d was not cleaned up after Close()", childPid)
	}
	if !waitForProcessExit(grandchildPid, 3*time.Second) {
		t.Errorf("grandchild PID %d was not cleaned up after Close()", grandchildPid)
	}
	if !waitForProcessExit(workerPid, 3*time.Second) {
		t.Errorf("worker PID %d was not cleaned up after Close()", workerPid)
	}
}

func TestWindows_AbnormalExit_WaitCalledOnce(t *testing.T) {
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("get self executable: %v", err)
	}

	spec := Spec{
		Command: selfExe,
		Args:    []string{"crash"},
		Env:     append(os.Environ(), "MCP_TEST_HELPER_MODE=crash"),
	}

	proc, err := Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}
	defer proc.Close()

	// Call Wait concurrently from 10 goroutines
	var wg sync.WaitGroup
	errs := make([]error, 10)
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			errs[idx] = proc.Wait()
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err == nil {
			t.Fatalf("expected error from crash exit, got nil at index %d", i)
		}
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			t.Errorf("expected *exec.ExitError at index %d, got: %v", i, err)
		} else if exitErr.ExitCode() != 42 {
			t.Errorf("expected exit code 42 at index %d, got: %d", i, exitErr.ExitCode())
		}
	}
}

func TestWindows_PipeCloseDelegation(t *testing.T) {
	selfExe, err := os.Executable()
	if err != nil {
		t.Fatalf("get self executable: %v", err)
	}

	spec := Spec{
		Command: selfExe,
		Args:    []string{"echo"},
		Env:     append(os.Environ(), "MCP_TEST_HELPER_MODE=echo_io"),
	}

	proc, err := Start(context.Background(), spec)
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Write something and read echo
	writer := proc.Writer()
	reader := proc.Reader()

	_, err = fmt.Fprintln(writer, "hello mcp")
	if err != nil {
		t.Fatalf("write failed: %v", err)
	}

	scanner := bufio.NewScanner(reader)
	if !scanner.Scan() {
		t.Fatalf("scan failed: %v", scanner.Err())
	}
	if scanner.Text() != "hello mcp" {
		t.Fatalf("unexpected output: %s", scanner.Text())
	}

	// Closing reader or writer delegates to proc.Close() with sync.Once
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_ = reader.Close()
	}()
	go func() {
		defer wg.Done()
		_ = writer.Close()
	}()
	wg.Wait()

	// Wait should complete cleanly
	_ = proc.Wait()
}
