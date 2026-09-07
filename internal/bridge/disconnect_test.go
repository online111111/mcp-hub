package bridge

import (
	"errors"
	"io"
	"testing"
)

func TestIsNormalLocalDisconnectSDK17EOF(t *testing.T) {
	for _, err := range []error{
		nil,
		io.EOF,
		io.ErrClosedPipe,
		errors.New("server is closing: EOF"),
	} {
		if !isNormalLocalDisconnect(err) {
			t.Fatalf("expected normal disconnect for %v", err)
		}
	}
	for _, err := range []error{
		errors.New("server is closing: permission denied"),
		errors.New("calling tools/call: EOF"),
	} {
		if isNormalLocalDisconnect(err) {
			t.Fatalf("unexpectedly swallowed error %v", err)
		}
	}
}
