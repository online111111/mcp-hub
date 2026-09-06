package runtime

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"mcp-hub/internal/inbound"
	"mcp-hub/internal/manager"
)

func TestControllerReloadNowTracksAppliedListenerChange(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	data := []byte(`{"version":1,"hub":{"listen":"127.0.0.1:9090"},"mcpServers":{}}`)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	controller := NewController(manager.NewManager(nil), path, "127.0.0.1:8080", nil)
	if err := controller.ReloadNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !controller.RestartRequired() {
		t.Fatal("listener change did not set restart-required state")
	}
	if got := controller.LastReloadStatus(); got != "restart_required (listen address changed)" {
		t.Fatalf("unexpected reload status %q", got)
	}
	restored := []byte(`{"version":1,"hub":{"listen":"127.0.0.1:8080"},"mcpServers":{}}`)
	if err := os.WriteFile(path, restored, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := controller.ReloadNow(context.Background()); err != nil {
		t.Fatal(err)
	}
	if controller.RestartRequired() {
		t.Fatal("restoring the active listener should clear restart-required state")
	}
}

func TestControllerDelegatesSanitizedRecentCalls(t *testing.T) {
	mgr := manager.NewManager(nil)
	want := inbound.RecentCallDTO{
		RequestID: "request-1",
		Tool:      "server__tool",
		ServerID:  "server",
		Outcome:   "success",
	}
	mgr.RecordCall(want)
	controller := NewController(mgr, filepath.Join(t.TempDir(), "missing.json"), "127.0.0.1:8080", nil)
	got := controller.GetRecentCalls()
	if len(got) != 1 || got[0] != want {
		t.Fatalf("unexpected recent calls: %+v", got)
	}
}

func TestControllerRequiresTwoStableSamples(t *testing.T) {
	controller := NewController(nil, filepath.Join(t.TempDir(), "missing.json"), "127.0.0.1:8080", nil)
	if controller.observeCandidate("digest-a") {
		t.Fatal("first changed sample must not be applied")
	}
	if !controller.observeCandidate("digest-a") {
		t.Fatal("second stable sample should be eligible")
	}
	if controller.observeCandidate("digest-b") {
		t.Fatal("a new candidate must restart sampling")
	}
}
