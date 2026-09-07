package bridge

import (
	"context"
	"errors"
	"io"
	"strings"
)

func isNormalLocalDisconnect(err error) bool {
	if err == nil || errors.Is(err, io.EOF) || errors.Is(err, io.ErrClosedPipe) || errors.Is(err, context.Canceled) {
		return true
	}

	// go-sdk v1.7 can format (rather than wrap) the underlying EOF while the
	// JSON-RPC server is closing, so errors.Is(err, io.EOF) cannot observe it.
	// Keep this workaround deliberately narrow until the upstream wrapper
	// preserves the cause.
	msg := err.Error()
	return strings.HasPrefix(msg, "server is closing:") && strings.HasSuffix(msg, ": EOF")
}
