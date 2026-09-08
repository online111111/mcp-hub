package runtime

import (
	"context"
	"github.com/online111111/mcp-manager/internal/config"
	"github.com/online111111/mcp-manager/internal/manager"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigTransactionExcludesPollUntilRestoration(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	original := []byte(`{"version":1,"mcpServers":{"original":{"enabled":false,"type":"streamable_http","url":"http://localhost/a"}}}`)
	changed := []byte(`{"version":1,"mcpServers":{}}`)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	mgr := manager.NewManager(nil)
	c := NewController(mgr, path, config.DefaultListen, nil)
	if err := c.ReloadNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	c.WithConfigTransaction(func(reload func(context.Context) error) {
		if err := os.WriteFile(path, changed, 0600); err != nil {
			t.Fatal(err)
		}
		if err := reload(context.Background()); err != nil {
			t.Fatal(err)
		}
		go func() { c.poll(context.Background()); c.poll(context.Background()); close(done) }()
		select {
		case <-done:
			t.Error("poll entered an uncommitted transaction")
		case <-time.After(100 * time.Millisecond):
		}
		if err := os.WriteFile(path, original, 0600); err != nil {
			t.Fatal(err)
		}
		if err := reload(context.Background()); err != nil {
			t.Fatal(err)
		}
	})
	<-done
	if states := mgr.GetServerStatuses(); len(states) != 1 || states[0].ID != "original" {
		t.Fatalf("restoration lost: %+v", states)
	}
}

func TestPollCannotOverwriteNewerReload(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	initial := []byte(`{"version":1,"mcpServers":{}}`)
	a := []byte(`{"version":1,"mcpServers":{"a":{"enabled":false,"type":"streamable_http","url":"http://localhost/a"}}}`)
	b := []byte(`{"version":1,"mcpServers":{"b":{"enabled":false,"type":"streamable_http","url":"http://localhost/b"}}}`)
	write := func(data []byte) {
		t.Helper()
		if err := os.WriteFile(path, data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write(initial)
	entered, release := make(chan struct{}), make(chan struct{})
	mgr := manager.NewManager(nil)
	c := NewController(mgr, path, config.DefaultListen, func(data []byte, path string) (*config.Config, *config.ResolvedConfig, error) {
		if string(data) == string(a) {
			close(entered)
			<-release
		}
		return config.Parse(data, filepath.Dir(path))
	})
	write(a)
	c.poll(context.Background())
	pollDone := make(chan struct{})
	go func() { c.poll(context.Background()); close(pollDone) }()
	<-entered
	write(b)
	reloadDone := make(chan error, 1)
	go func() { reloadDone <- c.ReloadNow(context.Background()) }()
	overtook := false
	select {
	case err := <-reloadDone:
		if err != nil {
			t.Fatal(err)
		}
		overtook = true
	case <-time.After(100 * time.Millisecond):
	}
	close(release)
	<-pollDone
	if !overtook {
		if err := <-reloadDone; err != nil {
			t.Fatal(err)
		}
	}
	statuses := mgr.GetServerStatuses()
	if len(statuses) != 1 || statuses[0].ID != "b" {
		t.Fatalf("stale poll overwrote latest runtime: %+v", statuses)
	}
	if c.appliedDigest != digest(b) {
		t.Fatal("digest does not match latest reload")
	}
}
