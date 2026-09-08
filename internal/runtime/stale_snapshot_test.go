package runtime

import (
	"context"
	"github.com/online111111/mcp-manager/internal/config"
	"github.com/online111111/mcp-manager/internal/manager"
	"os"
	"path/filepath"
	"testing"
)

func TestReloadRejectsSnapshotReplacedDuringParse(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	a := []byte(`{"version":1,"mcpServers":{"a":{"enabled":false,"type":"stdio","command":"node"}}}`)
	b := []byte(`{"version":1,"mcpServers":{"b":{"enabled":false,"type":"stdio","command":"node"}}}`)
	if err := os.WriteFile(path, a, 0600); err != nil {
		t.Fatal(err)
	}
	mgr := manager.NewManager(nil)
	c := NewController(mgr, path, config.DefaultListen, func(data []byte, path string) (*config.Config, *config.ResolvedConfig, error) {
		if err := os.WriteFile(path, b, 0600); err != nil {
			t.Fatal(err)
		}
		return config.Parse(data, filepath.Dir(path))
	})
	if err := c.ReloadNow(context.Background()); err == nil {
		t.Fatal("accepted a snapshot replaced by an external writer during parsing")
	}
	if got := mgr.GetServerStatuses(); len(got) != 0 {
		t.Fatalf("stale snapshot reached manager: %+v", got)
	}
}
