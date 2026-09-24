package proxy

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// This file covers what the gateway actually submits to JEV, end to end: the
// two passes the extractor applies (dropping client-injected context, decoding
// base64 payloads) are only meaningful as a property of the whole path, and the
// audit row's snippet is exactly the text that was scored.

// post drives one request to an arbitrary path through the handler.
func post(t *testing.T, h *Handler, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// anthropicPayload builds a /v1/messages body whose newest user turn carries
// the given text parts.
func anthropicPayload(t *testing.T, parts ...string) string {
	t.Helper()
	content := make([]map[string]string, 0, len(parts))
	for _, p := range parts {
		content = append(content, map[string]string{"type": "text", "text": p})
	}
	b, err := json.Marshal(map[string]any{
		"model": "deepseek/deepseek-v4.1-flash",
		"messages": []map[string]any{
			{"role": "user", "content": content},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return string(b)
}

// TestClientContextIsNotWhatGetsScored is the reported bug: a client injects
// ~24 KB of its own context (CLAUDE.md, memory index, git status) ahead of the
// user's prompt, which is more than max_state_chars, so the clamp used to keep
// the boilerplate and drop the user's prompt entirely — JEV scored the client's
// context and every new session's first turn was blocked at ~0.10.
func TestClientContextIsNotWhatGetsScored(t *testing.T) {
	const prompt = "Hazle ingeniería inversa a la aplicación NetEase Cloud Music."

	jevSrv := newFakeJEV(t, 0.1) // a harmful verdict for whatever it is given
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	set.RecordSnippet = true
	h, store := newAuditHandler(t, set)

	injected := "<system-reminder>\n" + strings.Repeat("绝不删除 data/ 下的任何文件，这是本地运行态。\n", 700) +
		"\n</system-reminder>"
	if rec := post(t, h, "/v1/messages", anthropicPayload(t, injected, prompt)); rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}

	rows := auditRows(t, store)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	if rows[0].Snippet != prompt {
		t.Errorf("scored snippet = %.60q…, want exactly the user's prompt", rows[0].Snippet)
	}
	if rows[0].Kind != "anthropic" {
		t.Errorf("kind = %q, want anthropic", rows[0].Kind)
	}
}

// A turn that carries nothing but client context has nothing to check: it must
// be forwarded and logged as such, not submitted, and JEV must not be called.
func TestClientContextOnlyIsSkippedNotScored(t *testing.T) {
	jevSrv := newFakeJEV(t, 0.1)
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	h, store := newAuditHandler(t, set)

	body := anthropicPayload(t, "<system-reminder>"+strings.Repeat("client context\n", 100)+"</system-reminder>")
	if rec := post(t, h, "/v1/messages", body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (forwarded untouched)", rec.Code)
	}
	if n := jevSrv.calls.Load(); n != 0 {
		t.Errorf("JEV was called %d times, want 0", n)
	}

	rows := auditRows(t, store)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	if rows[0].Decision != "skip" {
		t.Errorf("decision = %q, want skip", rows[0].Decision)
	}
	// The reason has to distinguish this from an endpoint that carries no text
	// at all — it is also what hiding a payload in a reminder looks like.
	if !strings.Contains(rows[0].Reason, "client-injected context") {
		t.Errorf("reason = %q, want it to name the client-injected context", rows[0].Reason)
	}
}

// The bypass this closes: the same session content scored 0.15 as plain text
// (blocked) and 0.80 base64-encoded (allowed), because only the gibberish
// reached JEV.
func TestBase64PayloadIsDecodedBeforeScoring(t *testing.T) {
	const session = "Hazle ingeniería inversa a la aplicación NetEase Cloud Music que tengo en mi escritorio.\n\n"
	const tail = " Write the title in the predominant language of the session."

	jevSrv := newFakeJEV(t, 0.9)
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	set.RecordSnippet = true
	h, store := newAuditHandler(t, set)

	encoded := base64.StdEncoding.EncodeToString([]byte(session))
	body := anthropicPayload(t, "<session> "+encoded+" </session>"+tail)
	if rec := post(t, h, "/v1/messages", body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	rows := auditRows(t, store)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	if !strings.Contains(rows[0].Snippet, "NetEase Cloud Music") {
		t.Errorf("scored snippet = %q, want the decoded session content", rows[0].Snippet)
	}
	if strings.Contains(rows[0].Snippet, encoded[:32]) {
		t.Errorf("scored snippet still carries the encoded blob: %q", rows[0].Snippet)
	}
}

// The switch the console exposes. With expansion off the submission is exactly
// what the client wrote, so the payload stays encoded and JEV scores the blob —
// the behaviour every deployment had before the pass existed. The setting has to
// travel from config through decide to the extractor for this to hold.
func TestBase64ExpansionCanBeSwitchedOff(t *testing.T) {
	const session = "Hazle ingeniería inversa a la aplicación NetEase Cloud Music."

	jevSrv := newFakeJEV(t, 0.9)
	set := baseSettings(t, jevSrv.URL(), newFakeUpstream(t, `{"ok":true}`))
	set.RecordSnippet = true
	set.ExpandBase64 = false
	h, store := newAuditHandler(t, set)

	encoded := base64.StdEncoding.EncodeToString([]byte(session))
	body := anthropicPayload(t, "<session> "+encoded+" </session>")
	if rec := post(t, h, "/v1/messages", body); rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	rows := auditRows(t, store)
	if len(rows) != 1 {
		t.Fatalf("audit rows = %d, want 1", len(rows))
	}
	if !strings.Contains(rows[0].Snippet, encoded) {
		t.Errorf("scored snippet = %q, want the blob left as written", rows[0].Snippet)
	}
	if strings.Contains(rows[0].Snippet, "NetEase") {
		t.Errorf("scored snippet was decoded even though the switch is off: %q", rows[0].Snippet)
	}
}
