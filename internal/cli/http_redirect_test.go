package cli

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// Even same-origin redirects are forbidden: authentication belongs only to the
// explicitly requested endpoint, not an arbitrary path selected by a response.
func TestHubHTTPClientRejectsRedirects(t *testing.T) {
	for _, topology := range []string{"cross-origin", "https-downgrade", "same-origin"} {
		for _, code := range []int{301, 302, 303, 307, 308} {
			t.Run(fmt.Sprintf("%s/%d", topology, code), func(t *testing.T) {
				var targetHits atomic.Int32
				var leakedToken atomic.Bool
				targetHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					targetHits.Add(1)
					leakedToken.Store(r.Header.Get("Authorization") != "")
					w.WriteHeader(http.StatusOK)
				})
				target := httptest.NewServer(targetHandler)
				defer target.Close()
				location := target.URL + "/stolen"
				if topology == "same-origin" {
					location = "/stolen"
				}
				var initialHits atomic.Int32
				handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					if r.URL.Path == "/stolen" {
						targetHandler.ServeHTTP(w, r)
						return
					}
					initialHits.Add(1)
					if r.Header.Get("Authorization") != "Bearer redirect-secret" {
						t.Error("initial request missing authentication")
					}
					w.Header().Set("Location", location)
					w.WriteHeader(code)
					_, _ = io.WriteString(w, "redirect response")
				})
				var source *httptest.Server
				if topology == "https-downgrade" {
					source = httptest.NewTLSServer(handler)
				} else {
					source = httptest.NewServer(handler)
				}
				defer source.Close()
				client := newHubHTTPClient("redirect-secret", time.Second)
				// Trust only the test server's certificate; retain the production
				// client and bearer transport, including its redirect policy.
				transport := client.Transport.(bearerTransport)
				transport.base = source.Client().Transport
				client.Transport = transport
				resp, err := client.Get(source.URL + "/api/v1/status")
				if err != nil {
					t.Fatal(err)
				}
				defer resp.Body.Close()
				if targetHits.Load() != 0 {
					t.Errorf("redirect target contacted %d times; token leaked: %v", targetHits.Load(), leakedToken.Load())
				}
				if initialHits.Load() != 1 || resp.StatusCode != code {
					t.Errorf("initial requests = %d, response status = %d; want 1 and %d", initialHits.Load(), resp.StatusCode, code)
				}
				body, err := io.ReadAll(resp.Body)
				if err != nil || string(body) != "redirect response" {
					t.Errorf("redirect response body = %q, err = %v", body, err)
				}
			})
		}
	}
}

func TestDiagnosticCommandsRejectRedirects(t *testing.T) {
	for _, tc := range []struct{ command, path string }{
		{"status", "/api/v1/status"},
		{"doctor", "/healthz"},
		{"doctor", "/readyz"},
		{"doctor", "/mcp"},
	} {
		t.Run(tc.command+tc.path, func(t *testing.T) {
			var targetHits atomic.Int32
			target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				targetHits.Add(1)
				_, _ = io.WriteString(w, `{}`)
			}))
			defer target.Close()
			var redirectHits atomic.Int32
			source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Header.Get("Authorization") != "Bearer command-secret" {
					t.Error("diagnostic request missing authentication")
				}
				if r.URL.Path == tc.path {
					redirectHits.Add(1)
					http.Redirect(w, r, target.URL, http.StatusTemporaryRedirect)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer source.Close()
			var stdout, stderr bytes.Buffer
			code := Run([]string{tc.command, "--endpoint", source.URL, "--token", "command-secret"}, &stdout, &stderr)
			if code != ExitRuntimeUnavailable {
				t.Errorf("exit = %d; want %d; stderr: %s", code, ExitRuntimeUnavailable, stderr.String())
			}
			if redirectHits.Load() == 0 || targetHits.Load() != 0 {
				t.Errorf("redirect responses = %d; target requests = %d; want >=1 and 0", redirectHits.Load(), targetHits.Load())
			}
			if strings.Contains(stdout.String()+stderr.String(), "command-secret") {
				t.Error("diagnostic output leaked token")
			}
		})
	}
}
