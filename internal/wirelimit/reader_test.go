package wirelimit

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type chunkedReader struct {
	io.Reader
	chunk int
}

func (r chunkedReader) Read(p []byte) (int, error) {
	if len(p) > r.chunk {
		p = p[:r.chunk]
	}
	return r.Reader.Read(p)
}

func TestFramingLimits(t *testing.T) {
	cases := []struct {
		name, data string
		framing    Framing
		limit      int
		fail       bool
	}{
		{"exact body", "12345678", WholeBody, 8, false},
		{"large body", "123456789", WholeBody, 8, true},
		{"many stdio messages", strings.Repeat("1234567\n", 100), JSONLines, 8, false},
		{"large stdio line", "12345678\n", JSONLines, 8, true},
		{"many events", strings.Repeat("data: 1\n\n", 100), SSEEvents, 9, false},
		{"CRLF events", strings.Repeat("data: 1\r\n\r\n", 100), SSEEvents, 11, false},
		{"multiline event", "data: 1\ndata: 2\n\n", SSEEvents, 12, true},
		{"unterminated event", "data: " + strings.Repeat("x", 100), SSEEvents, 16, true},
	}
	for _, tc := range cases {
		for _, chunk := range []int{1, 2, 7, 4096} {
			t.Run(tc.name, func(t *testing.T) {
				r := &reader{ReadCloser: io.NopCloser(chunkedReader{strings.NewReader(tc.data), chunk}), framing: tc.framing, limit: tc.limit, blankLine: true}
				data, err := io.ReadAll(r)
				if tc.fail {
					if !errors.Is(err, ErrMessageTooLarge) {
						t.Fatalf("wanted size error, got %v", err)
					}
					if _, err = r.Read(make([]byte, 1)); !errors.Is(err, ErrMessageTooLarge) {
						t.Fatal("limit error not sticky")
					}
				} else if err != nil || string(data) != tc.data {
					t.Fatalf("healthy stream changed: %v", err)
				}
			})
		}
	}
}
