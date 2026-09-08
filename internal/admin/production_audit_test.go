package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAdminBodyRejectsAmbiguousJSON(t *testing.T) {
	for _, data := range []string{`{"token":"one","token":"two"}`, `{"Token":"one"}`, `null`, `[]`} {
		var target struct {
			Token string `json:"token"`
		}
		req := httptest.NewRequest("POST", "/", strings.NewReader(data))
		if err := decodeJSONBody(req, &target); err == nil {
			t.Errorf("accepted ambiguous body %s", data)
		}
	}
}

func TestLoginRequiresJSONContentType(t *testing.T) {
	h, _ := newAdminTest(t)
	for _, typ := range []string{"", "text/plain", "application/x-www-form-urlencoded"} {
		r := httptest.NewRequest("POST", "http://localhost/api/admin/v1/auth/login", strings.NewReader(`{"token":"admin-secret"}`))
		r.Header.Set("Content-Type", typ)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		if w.Code != http.StatusUnsupportedMediaType {
			t.Errorf("type=%q got %d", typ, w.Code)
		}
	}
}

type auditDeadlineWriter struct {
	*httptest.ResponseRecorder
	deadlines []time.Time
}

func (w *auditDeadlineWriter) SetReadDeadline(deadline time.Time) error {
	w.deadlines = append(w.deadlines, deadline)
	return nil
}

func TestAdminMutationSetsAndResetsBodyDeadline(t *testing.T) {
	h, _ := newAdminTest(t)
	r := httptest.NewRequest("POST", "http://localhost/api/admin/v1/auth/login", strings.NewReader(`{"token":"admin-secret"}`))
	r.Header.Set("Content-Type", "application/json")
	w := &auditDeadlineWriter{ResponseRecorder: httptest.NewRecorder()}
	h.ServeHTTP(w, r)
	if len(w.deadlines) != 2 || w.deadlines[0].IsZero() || !w.deadlines[1].IsZero() {
		t.Fatalf("body deadline not installed/reset: %v", w.deadlines)
	}
}

func TestLocalAdminCannotDeleteFinalMCPToken(t *testing.T) {
	h, path := newAdminTest(t)
	cfg, _, err := h.readRawConfig()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Hub.Auth.BearerToken = strings.Repeat("m", 32)
	before, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, before, 0600); err != nil {
		t.Fatal(err)
	}
	_, digest, err := h.readRawConfig()
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("DELETE", "/api/admin/v1/tokens/0", nil)
	request.Header.Set("If-Match", `"`+digest+`"`)
	response := httptest.NewRecorder()
	h.deleteToken(response, request, 0)
	if response.Code != http.StatusBadRequest {
		t.Fatalf("final token deletion returned %d", response.Code)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(before) != string(after) {
		t.Fatal("refused deletion modified stored configuration")
	}
}
