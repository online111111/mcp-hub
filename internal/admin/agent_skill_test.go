package admin

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAgentDeploymentAssetsRequireAuthAndDownload(t *testing.T) {
	h, _ := newAdminTest(t)
	ts := httptest.NewServer(h)
	defer ts.Close()

	unauthorized, err := http.Get(ts.URL + "/api/admin/v1/agent-skill.zip")
	if err != nil {
		t.Fatal(err)
	}
	unauthorized.Body.Close()
	if unauthorized.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expected unauthenticated skill request to be rejected, got %d", unauthorized.StatusCode)
	}

	client, _ := login(t, ts)
	promptResp, err := client.Get(ts.URL + "/api/admin/v1/agent-prompt")
	if err != nil {
		t.Fatal(err)
	}
	prompt, err := io.ReadAll(promptResp.Body)
	promptResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if promptResp.StatusCode != http.StatusOK || !strings.Contains(string(prompt), "Deploy MCP Manager") {
		t.Fatalf("unexpected agent prompt response %d: %s", promptResp.StatusCode, prompt)
	}

	skillResp, err := client.Get(ts.URL + "/api/admin/v1/agent-skill.zip")
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(skillResp.Body)
	skillResp.Body.Close()
	if err != nil {
		t.Fatal(err)
	}
	if skillResp.StatusCode != http.StatusOK {
		t.Fatalf("skill download status %d: %s", skillResp.StatusCode, data)
	}
	if got := skillResp.Header.Get("Content-Disposition"); !strings.Contains(got, "skill.zip") {
		t.Fatalf("unexpected content disposition: %q", got)
	}

	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("invalid skill zip: %v", err)
	}
	files := make(map[string]bool, len(zr.File))
	for _, file := range zr.File {
		files[file.Name] = true
	}
	for _, required := range []string{
		"mcp-manager-deployer/SKILL.md",
		"mcp-manager-deployer/agents/openai.yaml",
		"mcp-manager-deployer/scripts/install_release.py",
		"mcp-manager-deployer/assets/copy-paste-agent-prompt.md",
	} {
		if !files[required] {
			t.Fatalf("skill zip missing %s", required)
		}
	}
}
