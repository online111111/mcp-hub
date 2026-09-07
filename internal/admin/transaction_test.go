package admin

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"mcp-hub/internal/config"
	"mcp-hub/internal/manager"
	hubruntime "mcp-hub/internal/runtime"
)

// Exercise actual Manager reconciliation on the rejected config and restoration,
// not a reload mock that leaves the runtime unchanged.
func TestConcurrentWriteCannotEscapeFailedReloadRollback(t *testing.T) {
	h, path := newAdminTest(t)
	original, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	mgr := manager.NewManager(nil)
	c := hubruntime.NewController(mgr, path, config.DefaultListen, nil)
	if err := c.ReloadNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	h.opts.ConfigTransaction = func(fn func(func(context.Context) error)) {
		c.WithConfigTransaction(func(reload func(context.Context) error) {
			fn(func(ctx context.Context) error {
				if err := reload(ctx); err != nil {
					return err
				}
				data, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				if !strings.Contains(string(data), `"remote"`) && !strings.Contains(string(data), `"next"`) {
					close(entered)
					<-release
					return errors.New("failure after reconciliation")
				}
				return nil
			})
		})
	}
	first := httptest.NewRequest(http.MethodDelete, "/", strings.NewReader(`{}`))
	first.Header.Set("If-Match", config.ComputeDigest(original))
	firstResp := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { h.deleteServer(firstResp, first, "remote"); close(done) }()
	<-entered
	intermediate, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second := httptest.NewRequest(http.MethodPut, "/", strings.NewReader(`{"enabled":false,"type":"streamable_http","url":"http://localhost/next"}`))
	second.Header.Set("If-Match", config.ComputeDigest(intermediate))
	secondResp := httptest.NewRecorder()
	secondDone := make(chan struct{})
	go func() { h.putServer(secondResp, second, "next"); close(secondDone) }()
	escaped := false
	select {
	case <-secondDone:
		escaped = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-done
	<-secondDone
	if escaped {
		t.Errorf("concurrent write committed before failed transaction rolled back")
	}
	if firstResp.Code != http.StatusServiceUnavailable {
		t.Errorf("rollback response: %d %s", firstResp.Code, firstResp.Body.String())
	}
	if secondResp.Code != http.StatusConflict {
		t.Errorf("stale intermediate ETag accepted: %d", secondResp.Code)
	}
	restored, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(restored) != string(original) {
		t.Error("disk was not restored")
	}
	statuses := mgr.GetServerStatuses()
	if len(statuses) != 1 || statuses[0].ID != "remote" {
		t.Fatalf("real manager not restored: %+v", statuses)
	}
}
