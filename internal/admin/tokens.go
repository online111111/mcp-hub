package admin

import (
	"context"
	"net/http"
	"os"
	"strings"

	"github.com/online111111/mcp-manager/internal/config"
)

type tokenDTO struct {
	Index  int    `json:"index"`
	Token  string `json:"token"`
	Legacy bool   `json:"legacy"`
}

func (h *Handler) getTokens(w http.ResponseWriter) {
	cfg, digest, err := h.readRawConfig()
	if err != nil {
		http.Error(w, "configuration unavailable", http.StatusInternalServerError)
		return
	}

	tokens, err := resolvedTokenDTOs(cfg.Hub.Auth)
	if err != nil {
		http.Error(w, "MCP access tokens unavailable", http.StatusInternalServerError)
		return
	}
	w.Header().Set("ETag", `"`+digest+`"`)
	w.Header().Set("Cache-Control", "no-store")
	h.writeJSON(w, http.StatusOK, map[string]any{"tokens": tokens})
}

func resolvedTokenDTOs(auth config.HubAuthConfig) ([]tokenDTO, error) {
	tokens := make([]tokenDTO, 0, 1+len(auth.BearerTokens))
	index := 0
	if strings.TrimSpace(auth.BearerToken) != "" {
		value, err := config.ExpandEnv(auth.BearerToken, os.LookupEnv)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tokenDTO{Index: index, Token: value, Legacy: true})
		index++
	}
	for _, raw := range auth.BearerTokens {
		value, err := config.ExpandEnv(raw, os.LookupEnv)
		if err != nil {
			return nil, err
		}
		tokens = append(tokens, tokenDTO{Index: index, Token: value})
		index++
	}
	return tokens, nil
}

func (h *Handler) postToken(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Token string `json:"token"`
	}
	if err := decodeJSONBody(r, &body); err != nil {
		http.Error(w, "invalid token request", http.StatusBadRequest)
		return
	}
	candidate := strings.TrimSpace(body.Token)
	if candidate == "" {
		generated, err := randomToken()
		if err != nil {
			http.Error(w, "token generation failed", http.StatusInternalServerError)
			return
		}
		candidate = generated
	}
	if len(candidate) < config.MinAuthTokenLength {
		http.Error(w, "MCP token must be at least 32 characters", http.StatusBadRequest)
		return
	}
	if strings.ContainsAny(candidate, "\r\n") {
		http.Error(w, "MCP token must be a single line", http.StatusBadRequest)
		return
	}

	h.withConfigTransaction(func(reload func(context.Context) error) {
		cfg, digest, err := h.readRawConfig()
		if err != nil {
			http.Error(w, "configuration unavailable", http.StatusInternalServerError)
			return
		}
		if !matchETag(r.Header.Get("If-Match"), digest) {
			http.Error(w, "configuration changed", http.StatusConflict)
			return
		}
		for _, existing := range appendTokenValues(cfg.Hub.Auth) {
			if value, expandErr := config.ExpandEnv(existing, os.LookupEnv); expandErr == nil && value == candidate {
				http.Error(w, "MCP token already exists", http.StatusConflict)
				return
			}
		}
		cfg.Hub.Auth.BearerTokens = append(cfg.Hub.Auth.BearerTokens, candidate)
		h.writeConfigLocked(w, r, cfg, digest, reload)
	})
}

func (h *Handler) deleteToken(w http.ResponseWriter, r *http.Request, index int) {
	h.withConfigTransaction(func(reload func(context.Context) error) {
		cfg, digest, err := h.readRawConfig()
		if err != nil {
			http.Error(w, "configuration unavailable", http.StatusInternalServerError)
			return
		}
		if !matchETag(r.Header.Get("If-Match"), digest) {
			http.Error(w, "configuration changed", http.StatusConflict)
			return
		}

		legacyCount := 0
		if strings.TrimSpace(cfg.Hub.Auth.BearerToken) != "" {
			legacyCount = 1
		}
		total := legacyCount + len(cfg.Hub.Auth.BearerTokens)
		if index < 0 || index >= total {
			http.Error(w, "MCP token not found", http.StatusNotFound)
			return
		}
		if legacyCount == 1 && index == 0 {
			cfg.Hub.Auth.BearerToken = ""
		} else {
			listIndex := index - legacyCount
			cfg.Hub.Auth.BearerTokens = append(cfg.Hub.Auth.BearerTokens[:listIndex], cfg.Hub.Auth.BearerTokens[listIndex+1:]...)
		}
		h.writeConfigLocked(w, r, cfg, digest, reload)
	})
}

func appendTokenValues(auth config.HubAuthConfig) []string {
	values := make([]string, 0, 1+len(auth.BearerTokens))
	if strings.TrimSpace(auth.BearerToken) != "" {
		values = append(values, auth.BearerToken)
	}
	values = append(values, auth.BearerTokens...)
	return values
}
