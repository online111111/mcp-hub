package admin

import (
	"archive/zip"
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed agent-skill
var agentSkillFiles embed.FS

func (h *Handler) getAgentPrompt(w http.ResponseWriter) {
	data, err := agentSkillFiles.ReadFile("agent-skill/assets/copy-paste-agent-prompt.md")
	if err != nil {
		http.Error(w, "agent prompt unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (h *Handler) getAgentSkill(w http.ResponseWriter) {
	sub, err := fs.Sub(agentSkillFiles, "agent-skill")
	if err != nil {
		http.Error(w, "agent skill unavailable", http.StatusInternalServerError)
		return
	}

	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	err = fs.WalkDir(sub, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() {
			return nil
		}
		data, readErr := fs.ReadFile(sub, name)
		if readErr != nil {
			return readErr
		}
		writer, createErr := zw.Create(path.Join("mcp-manager-deployer", name))
		if createErr != nil {
			return createErr
		}
		_, writeErr := writer.Write(data)
		return writeErr
	})
	if err == nil {
		err = zw.Close()
	} else {
		_ = zw.Close()
	}
	if err != nil {
		http.Error(w, "agent skill unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="skill.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", stringContentLength(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}

func stringContentLength(size int) string {
	if size == 0 {
		return "0"
	}
	var digits [20]byte
	pos := len(digits)
	for size > 0 {
		pos--
		digits[pos] = byte('0' + size%10)
		size /= 10
	}
	return string(digits[pos:])
}
