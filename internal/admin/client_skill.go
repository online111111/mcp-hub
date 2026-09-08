package admin

import (
	"archive/zip"
	"bytes"
	"embed"
	"io/fs"
	"net/http"
	"path"
)

//go:embed client-skill
var clientSkillFiles embed.FS

func (h *Handler) getClientPrompt(w http.ResponseWriter) {
	data, err := clientSkillFiles.ReadFile("client-skill/assets/copy-paste-client-prompt.md")
	if err != nil {
		http.Error(w, "client prompt unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(data)
}

func (h *Handler) getClientSkill(w http.ResponseWriter) {
	sub, err := fs.Sub(clientSkillFiles, "client-skill")
	if err != nil {
		http.Error(w, "client skill unavailable", http.StatusInternalServerError)
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
		writer, createErr := zw.Create(path.Join("mcp-manager-connector", name))
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
		http.Error(w, "client skill unavailable", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", `attachment; filename="skill.zip"`)
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("Content-Length", stringContentLength(buf.Len()))
	_, _ = w.Write(buf.Bytes())
}
