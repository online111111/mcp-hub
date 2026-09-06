package manager

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeErrorDoesNotLeakEndpointOrPath(t *testing.T) {
	secretURL := `http://127.0.0.1:19999/private-api/secret-path`
	secretPath := `C:\Users\private-user\server.js`

	cases := []error{
		errors.New(`calling "initialize": Post "` + secretURL + `": connectex: connection refused`),
		errors.New(`launch stdio process "` + secretPath + `": file does not exist`),
	}

	for _, err := range cases {
		got := sanitizeError(err)
		if got == "" {
			t.Fatal("expected non-empty error category")
		}
		if strings.Contains(got, secretURL) || strings.Contains(got, secretPath) || strings.Contains(got, "private-user") {
			t.Fatalf("sanitized error leaked sensitive detail: %q", got)
		}
	}
}
