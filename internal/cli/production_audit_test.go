package cli

import (
	"io"
	"testing"
)

func TestRepeatedAdminCommandsReleaseSessions(t *testing.T) {
	server := newRemoteAdminCLITestServer(t)
	for i := 0; i < 24; i++ {
		code := runAdmin([]string{"list", "--endpoint", server.URL, "--token", "admin-secret"}, io.Discard, io.Discard)
		if code != ExitSuccess {
			t.Fatalf("command %d exhausted Admin session capacity (exit %d)", i+1, code)
		}
	}
}
