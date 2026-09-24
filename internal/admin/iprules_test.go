package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"jev-safety-gateway/internal/config"
)

// bodyWithToken issues a request carrying a JSON body and (optionally) a bearer
// token — the write-method companion to getWithToken.
func bodyWithToken(t *testing.T, h *Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, jsonBody(t, body))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	return w
}

// The rule endpoints mutate the proxy's pre-JEV reject path, so like every other
// /api/ route they must reject an anonymous caller.
func TestIPRulesRequireToken(t *testing.T) {
	h, _ := newTestHandler(t)
	if w := getWithToken(t, h, "/api/ip-rules", ""); w.Code != http.StatusUnauthorized {
		t.Errorf("GET /api/ip-rules without a token = %d, want 401", w.Code)
	}
}

func TestIPRulesCreateListDelete(t *testing.T) {
	h, _ := newTestHandler(t)
	token := loginToken(t, h)

	w := bodyWithToken(t, h, http.MethodPost, "/api/ip-rules", token, map[string]any{
		"pattern": "1.2.3.4", "kind": "block", "duration_sec": 600, "reason": "manual",
	})
	if w.Code != http.StatusOK {
		t.Fatalf("create block = %d, body = %s", w.Code, w.Body.String())
	}
	var rule config.IPRule
	if err := json.Unmarshal(w.Body.Bytes(), &rule); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	if rule.ID == 0 || rule.Pattern != "1.2.3.4" || rule.ExpiresAt == nil {
		t.Fatalf("unexpected created rule: %+v", rule)
	}

	if w := bodyWithToken(t, h, http.MethodPost, "/api/ip-rules", token, map[string]any{
		"pattern": "10.0.0.0/8", "kind": "allow", "reason": "trusted",
	}); w.Code != http.StatusOK {
		t.Fatalf("create allow = %d, body = %s", w.Code, w.Body.String())
	}

	var list struct {
		Items []config.IPRule `json:"items"`
	}
	w = getWithToken(t, h, "/api/ip-rules", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d, body = %s", w.Code, w.Body.String())
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(list.Items))
	}

	if w := bodyWithToken(t, h, http.MethodDelete, "/api/ip-rules/"+strconv.FormatInt(rule.ID, 10), token, nil); w.Code != http.StatusOK {
		t.Fatalf("delete = %d, body = %s", w.Code, w.Body.String())
	}
	w = getWithToken(t, h, "/api/ip-rules", token)
	list.Items = nil
	_ = json.Unmarshal(w.Body.Bytes(), &list)
	if len(list.Items) != 1 || list.Items[0].Kind != "allow" {
		t.Fatalf("after delete items = %+v, want only the allow rule", list.Items)
	}
}

// Bad input is rejected with 400 rather than persisting a rule the proxy would
// then try to match.
func TestIPRulesValidation(t *testing.T) {
	h, _ := newTestHandler(t)
	token := loginToken(t, h)
	cases := []map[string]any{
		{"pattern": "1.2.3.4", "kind": "block"},                       // temp block without a duration
		{"pattern": "bad", "kind": "block", "duration_sec": 60},       // invalid pattern
		{"pattern": "1.2.3.4", "kind": "sideways", "permanent": true}, // invalid kind
	}
	for i, c := range cases {
		if w := bodyWithToken(t, h, http.MethodPost, "/api/ip-rules", token, c); w.Code != http.StatusBadRequest {
			t.Errorf("case %d %v = %d, want 400 (body %s)", i, c, w.Code, w.Body.String())
		}
	}
}
