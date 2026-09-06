package testserver

import (
	"fmt"
	"net"
	"sync"
)

// ExclusiveListener holds a local TCP listener to verify exclusive port binding
// and detect double-instance / port collision per Agent实施手册 T03.
type ExclusiveListener struct {
	mu       sync.Mutex
	listener net.Listener
	port     int
	addr     string
	closed   bool
}

// AcquireExclusivePort binds a local TCP listener to 127.0.0.1 on the specified port.
// If port is 0, the OS allocates an ephemeral available port.
// The listener remains active until Close() is called.
func AcquireExclusivePort(port int) (*ExclusiveListener, error) {
	bindAddr := fmt.Sprintf("127.0.0.1:%d", port)
	ln, err := net.Listen("tcp", bindAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to bind exclusive port %d: %w", port, err)
	}
	tcpAddr, ok := ln.Addr().(*net.TCPAddr)
	if !ok {
		_ = ln.Close()
		return nil, fmt.Errorf("unexpected listener address type: %T", ln.Addr())
	}
	return &ExclusiveListener{
		listener: ln,
		port:     tcpAddr.Port,
		addr:     tcpAddr.String(),
	}, nil
}

// Port returns the bound port number.
func (l *ExclusiveListener) Port() int {
	return l.port
}

// Addr returns the bound address string (e.g. "127.0.0.1:12345").
func (l *ExclusiveListener) Addr() string {
	return l.addr
}

// CheckCollision attempts to bind a second listener to the same address.
// It returns nil if the collision was correctly detected (the second bind failed),
// or an error if the second bind unexpectedly succeeded.
func (l *ExclusiveListener) CheckCollision() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return fmt.Errorf("cannot check collision on closed listener")
	}

	ln2, err := net.Listen("tcp", l.addr)
	if err == nil {
		_ = ln2.Close()
		return fmt.Errorf("port collision check failed: secondary listen on %s unexpectedly succeeded", l.addr)
	}
	// Collision successfully confirmed (bind rejected as expected)
	return nil
}

// Close closes the exclusive listener and releases the port.
func (l *ExclusiveListener) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.closed {
		return nil
	}
	l.closed = true
	if l.listener != nil {
		return l.listener.Close()
	}
	return nil
}
