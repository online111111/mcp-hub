package process

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"
)

func TestBootstrap_RoundTrip(t *testing.T) {
	orig := &BootstrapMessage{
		Command: `C:\tools\server.exe`,
		Args:    []string{"--port", "8080", "--name", "test-server"},
		Dir:     `C:\workspace`,
		Env:     []string{"LOG_LEVEL=debug", "PORT=8080"},
	}

	var buf bytes.Buffer
	if err := WriteBootstrap(&buf, orig); err != nil {
		t.Fatalf("WriteBootstrap failed: %v", err)
	}

	readMsg, br, err := ReadBootstrap(&buf, 2*time.Second)
	if err != nil {
		t.Fatalf("ReadBootstrap failed: %v", err)
	}
	if br == nil {
		t.Fatalf("expected non-nil bufio.Reader")
	}

	if readMsg.Command != orig.Command {
		t.Errorf("Command mismatch: got %q, want %q", readMsg.Command, orig.Command)
	}
	if len(readMsg.Args) != len(orig.Args) {
		t.Fatalf("Args length mismatch: got %d, want %d", len(readMsg.Args), len(orig.Args))
	}
	for i := range orig.Args {
		if readMsg.Args[i] != orig.Args[i] {
			t.Errorf("Args[%d] mismatch: got %q, want %q", i, readMsg.Args[i], orig.Args[i])
		}
	}
	if readMsg.Dir != orig.Dir {
		t.Errorf("Dir mismatch: got %q, want %q", readMsg.Dir, orig.Dir)
	}
	if len(readMsg.Env) != len(orig.Env) {
		t.Fatalf("Env length mismatch: got %d, want %d", len(readMsg.Env), len(orig.Env))
	}
}

func TestBootstrap_PreReadBytesPreserved(t *testing.T) {
	// Simulate Hub writing bootstrap JSON line followed immediately by protocol data.
	// The buffered reader inside ReadBootstrap will read ahead, but those bytes
	// MUST remain available in the returned *bufio.Reader.
	msg := &BootstrapMessage{
		Command: `C:\bin\server.exe`,
		Args:    []string{"start"},
	}

	var buf bytes.Buffer
	if err := WriteBootstrap(&buf, msg); err != nil {
		t.Fatalf("WriteBootstrap failed: %v", err)
	}

	protocolPayload := "{\"jsonrpc\":\"2.0\",\"method\":\"initialize\"}\n{\"jsonrpc\":\"2.0\",\"id\":1}"
	buf.WriteString(protocolPayload)

	readMsg, br, err := ReadBootstrap(&buf, 2*time.Second)
	if err != nil {
		t.Fatalf("ReadBootstrap failed: %v", err)
	}
	if readMsg.Command != msg.Command {
		t.Errorf("Command mismatch: %q", readMsg.Command)
	}

	// Now read all remaining bytes from br
	remaining, err := io.ReadAll(br)
	if err != nil {
		t.Fatalf("reading remaining bytes from br failed: %v", err)
	}

	if string(remaining) != protocolPayload {
		t.Fatalf("pre-read bytes corrupted or lost!\nGot:  %q\nWant: %q", string(remaining), protocolPayload)
	}
}

func TestBootstrap_SizeLimit(t *testing.T) {
	// Create a payload larger than MaxBootstrapBytes (1 MiB)
	hugePayload := strings.Repeat("A", MaxBootstrapBytes+100)
	hugeJSON := `{"command":"` + hugePayload + `"}` + "\n"

	reader := strings.NewReader(hugeJSON)
	_, _, err := ReadBootstrap(reader, 2*time.Second)
	if err == nil {
		t.Fatalf("expected error for bootstrap exceeding 1 MiB limit, got nil")
	}
	if !strings.Contains(err.Error(), "limit") && !strings.Contains(err.Error(), "exceeds") {
		t.Errorf("expected error about size limit, got: %v", err)
	}
}

func TestBootstrap_Timeout(t *testing.T) {
	// Pipe where writer never writes. ReadBootstrap must close the readable end
	// on timeout so the internal reader goroutine can exit.
	pr, pw := io.Pipe()
	defer pw.Close()

	shortTimeout := 100 * time.Millisecond
	_, _, err := ReadBootstrap(pr, shortTimeout)
	if err == nil {
		t.Fatalf("expected timeout error, got nil")
	}
	if !errors.Is(err, ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
	if _, writeErr := pw.Write([]byte("x")); writeErr == nil {
		t.Fatal("expected writer to observe closed reader after timeout")
	}
}

func TestBootstrap_MalformedJSON(t *testing.T) {
	reader := strings.NewReader("{invalid-json\n")
	_, _, err := ReadBootstrap(reader, 2*time.Second)
	if err == nil {
		t.Fatalf("expected error for malformed json, got nil")
	}
}

func TestBootstrap_MissingCommand(t *testing.T) {
	reader := strings.NewReader(`{"dir":"C:\\workspace"}` + "\n")
	_, _, err := ReadBootstrap(reader, 2*time.Second)
	if err == nil {
		t.Fatalf("expected error for missing command, got nil")
	}
}

func TestBootstrapPreservesExplicitEmptyEnvironment(t *testing.T) {
	var data bytes.Buffer
	if err := WriteBootstrap(&data, &BootstrapMessage{Command: "server", Env: []string{}}); err != nil {
		t.Fatal(err)
	}
	message, _, err := ReadBootstrap(&data, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if message.Env == nil {
		t.Fatal("empty environment became nil and would inherit parent credentials")
	}
}
