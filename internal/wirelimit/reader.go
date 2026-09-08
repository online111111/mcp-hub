// Package wirelimit bounds each protocol message, not the lifetime of a stream.
package wirelimit

import (
	"errors"
	"io"
)

const MaxMessageBytes = 8 * 1024 * 1024

var ErrMessageTooLarge = errors.New("protocol message exceeds 8 MiB limit")

type Framing uint8

const (
	WholeBody Framing = iota
	JSONLines
	SSEEvents
)

type reader struct {
	io.ReadCloser
	framing   Framing
	limit     int
	used      int
	blankLine bool
	exceeded  bool
}

// New preserves Close ownership. JSON/error bodies have a cumulative limit;
// stdio resets at LF, while SSE resets only at a blank event-delimiter line.
// CRLF and LF are supported. A long healthy SSE connection is never limited by
// the total number of bytes exchanged across separate events.
func New(body io.ReadCloser, framing Framing) io.ReadCloser {
	return &reader{ReadCloser: body, framing: framing, limit: MaxMessageBytes, blankLine: true}
}

func (r *reader) Read(p []byte) (int, error) {
	if r.exceeded {
		return 0, ErrMessageTooLarge
	}
	n, err := r.ReadCloser.Read(p)
	for i, b := range p[:n] {
		r.used++
		if r.used > r.limit {
			r.exceeded = true
			return i, ErrMessageTooLarge
		}
		switch r.framing {
		case JSONLines:
			if b == '\n' {
				r.used = 0
			}
		case SSEEvents:
			if b == '\n' {
				if r.blankLine {
					r.used = 0
				}
				r.blankLine = true
			} else if b != '\r' {
				r.blankLine = false
			}
		}
	}
	return n, err
}
