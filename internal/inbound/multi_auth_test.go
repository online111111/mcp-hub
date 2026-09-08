package inbound

import (
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAuthorizedBearerAcceptsAnyConfiguredToken(t *testing.T) {
	one := strings.Repeat("a", 32)
	two := strings.Repeat("b", 32)
	s := &HTTPServer{publicMode: true, bearerTokens: []string{one, two}}
	for _, token := range []string{one, two} {
		req := httptest.NewRequest("GET", "https://mcp.example/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		if !s.authorizedBearer(req) {
			t.Fatalf("configured token was rejected: %q", token)
		}
	}
	req := httptest.NewRequest("GET", "https://mcp.example/mcp", nil)
	req.Header.Set("Authorization", "Bearer "+strings.Repeat("x", 32))
	if s.authorizedBearer(req) {
		t.Fatal("unknown token was accepted")
	}
}

func TestAuthorizedBearerUsesHotTokenProvider(t *testing.T) {
	one := strings.Repeat("c", 32)
	two := strings.Repeat("d", 32)
	active := []string{one}
	s := &HTTPServer{publicMode: true, bearerTokensProvider: func() []string { return active }}

	request := func(token string) bool {
		req := httptest.NewRequest("GET", "https://mcp.example/mcp", nil)
		req.Header.Set("Authorization", "Bearer "+token)
		return s.authorizedBearer(req)
	}
	if !request(one) || request(two) {
		t.Fatal("unexpected initial provider authorization state")
	}
	active = []string{two}
	if request(one) || !request(two) {
		t.Fatal("provider token rotation was not applied")
	}
}
