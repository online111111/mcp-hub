package admin

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientPromptIsClientOnly(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Handler{}).getClientPrompt(recorder)
	if recorder.Code != 200 {
		t.Fatalf("prompt status %d", recorder.Code)
	}
	body := recorder.Body.String()
	for _, expected := range []string{"{{MCP_ENDPOINT}}", "{{MCP_TOKEN}}", "不要部署", "当前这台客户端电脑"} {
		if !strings.Contains(body, expected) {
			t.Fatalf("client prompt missing %q", expected)
		}
	}
}

func TestClientSkillZipUsesConnectorRoot(t *testing.T) {
	recorder := httptest.NewRecorder()
	(&Handler{}).getClientSkill(recorder)
	if recorder.Code != 200 {
		t.Fatalf("skill status %d: %s", recorder.Code, recorder.Body.String())
	}
	zr, err := zip.NewReader(bytes.NewReader(recorder.Body.Bytes()), int64(recorder.Body.Len()))
	if err != nil {
		t.Fatal(err)
	}
	foundSkill := false
	for _, file := range zr.File {
		if !strings.HasPrefix(file.Name, "mcp-manager-connector/") {
			t.Fatalf("unexpected zip path %q", file.Name)
		}
		if file.Name == "mcp-manager-connector/SKILL.md" {
			foundSkill = true
			rc, err := file.Open()
			if err != nil {
				t.Fatal(err)
			}
			data, _ := io.ReadAll(rc)
			_ = rc.Close()
			if !strings.Contains(string(data), "name: mcp-manager-connector") {
				t.Fatal("connector skill frontmatter missing")
			}
		}
	}
	if !foundSkill {
		t.Fatal("SKILL.md missing from connector archive")
	}
}
