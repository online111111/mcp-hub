package netpolicy

import "testing"

func TestValidateHubEndpoint(t *testing.T) {
	for _, raw := range []string{
		"http://127.0.0.1:8080/mcp",
		"http://[::1]:8080/mcp",
		"http://localhost:8080/mcp",
		"https://hub.example.com/mcp",
	} {
		if _, err := ValidateHubEndpoint(raw); err != nil {
			t.Fatalf("%s: %v", raw, err)
		}
	}

	for _, raw := range []string{
		"http://hub.example.com/mcp",
		"https://user:pass@hub.example.com/mcp",
		"https://hub.example.com/mcp?token=secret",
		"https://hub.example.com/mcp#fragment",
		"ftp://hub.example.com/mcp",
	} {
		if _, err := ValidateHubEndpoint(raw); err == nil {
			t.Fatalf("expected %s to be rejected", raw)
		}
	}
}

func TestSafeURLRedactsCredentialsAndQuery(t *testing.T) {
	got := SafeURL("https://user:pass@example.com/mcp?token=secret#frag")
	if got != "https://example.com/mcp" {
		t.Fatalf("SafeURL = %q", got)
	}
}
