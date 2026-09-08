package process

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	// MaxBootstrapBytes is the maximum allowed size of the bootstrap JSON line (1 MiB).
	MaxBootstrapBytes = 1024 * 1024
	// DefaultBootstrapTimeout is the timeout for receiving the bootstrap line (5 seconds).
	DefaultBootstrapTimeout = 5 * time.Second
)

// BootstrapMessage represents the bootstrap payload sent from Hub to worker.
type BootstrapMessage struct {
	Command string   `json:"command"`
	Args    []string `json:"args,omitempty"`
	Dir     string   `json:"dir,omitempty"`
	Env     []string `json:"env"`
}

// WriteBootstrap serializes the BootstrapMessage as a single JSON line terminated by '\n'
// and writes it to w.
func WriteBootstrap(w io.Writer, msg *BootstrapMessage) error {
	if msg == nil {
		return errors.New("bootstrap message cannot be nil")
	}
	if msg.Command == "" {
		return errors.New("bootstrap message command cannot be empty")
	}
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal bootstrap message: %w", err)
	}
	if len(data)+1 > MaxBootstrapBytes {
		return fmt.Errorf("bootstrap message size (%d bytes) exceeds 1 MiB limit", len(data)+1)
	}
	data = append(data, '\n')
	if _, err := w.Write(data); err != nil {
		return fmt.Errorf("write bootstrap message: %w", err)
	}
	return nil
}

// ReadBootstrap reads the bootstrap message from r with a bounded size and timeout.
// It returns the parsed BootstrapMessage and the *bufio.Reader which MUST be reused
// for downstream stdin to preserve any pre-read bytes.
func ReadBootstrap(r io.Reader, timeout time.Duration) (*BootstrapMessage, *bufio.Reader, error) {
	if timeout <= 0 {
		timeout = DefaultBootstrapTimeout
	}

	br := bufio.NewReader(r)

	type readResult struct {
		data []byte
		err  error
	}

	ch := make(chan readResult, 1)
	go func() {
		var buf bytes.Buffer
		for {
			b, err := br.ReadByte()
			if err != nil {
				ch <- readResult{buf.Bytes(), err}
				return
			}
			if b == '\n' {
				ch <- readResult{buf.Bytes(), nil}
				return
			}
			if buf.Len() >= MaxBootstrapBytes {
				ch <- readResult{nil, fmt.Errorf("bootstrap message exceeds %d bytes limit", MaxBootstrapBytes)}
				return
			}
			buf.WriteByte(b)
		}
	}()

	var res readResult
	select {
	case res = <-ch:
		if res.err != nil {
			return nil, br, fmt.Errorf("read bootstrap message: %w", res.err)
		}
	case <-time.After(timeout):
		// Unblock the reader goroutine when the input supports Close (worker stdin
		// and io.Pipe both do). Without this, a silent peer could leave a blocked
		// ReadByte goroutine behind after the timeout returned.
		if closer, ok := r.(io.Closer); ok {
			_ = closer.Close()
		}
		return nil, br, fmt.Errorf("%w: bootstrap line not received within %v", ErrTimeout, timeout)
	}

	if len(res.data) == 0 {
		return nil, br, errors.New("empty bootstrap message")
	}

	var msg BootstrapMessage
	if err := json.Unmarshal(res.data, &msg); err != nil {
		return nil, br, fmt.Errorf("parse bootstrap json: %w", err)
	}
	if msg.Command == "" {
		return nil, br, errors.New("bootstrap message missing required command")
	}

	return &msg, br, nil
}
