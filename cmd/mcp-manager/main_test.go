package main

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestMainWorkerDispatch(t *testing.T) {
	if os.Getenv("MCP_MANAGER_MAIN_WORKER_TEST") == "1" {
		main()
		return
	}

	cmd := exec.Command(os.Args[0], "_worker")
	cmd.Env = append(os.Environ(), "MCP_MANAGER_MAIN_WORKER_TEST=1")
	cmd.Stdin = strings.NewReader("\n")
	output, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatal("expected invalid bootstrap to fail")
	}
	if strings.Contains(string(output), "unknown command") {
		t.Fatalf("worker mode reached CLI dispatcher instead of worker bootstrap: %s", output)
	}
	if !strings.Contains(string(output), "worker bootstrap failed") {
		t.Fatalf("expected worker bootstrap error, got: %s", output)
	}
}
