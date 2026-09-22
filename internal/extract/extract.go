// Package extract pulls the user-provided input text out of an upstream API
// request body, based on the request path. It understands the common
// OpenAI-compatible, Anthropic and Gemini shapes that new-api relays, and falls
// back to a generic string collector for anything unrecognized.
package extract

import (
	"encoding/json"
	"strings"
)

// Result is the outcome of extracting input from a request.
type Result struct {
	Text      string // concatenated user input to evaluate
	Checkable bool   // false => skip the JEV check (unknown/binary/empty)
	Kind      string // short label for logging
	Model     string // model name from the body, if present, for logging
}

// Extract picks the user-input text from body based on path.
func Extract(path string, body []byte) Result {
	p := strings.ToLower(path)
	model := peekModel(body)

	var r Result
	switch {
	case strings.HasSuffix(p, "/chat/completions"):
		r = extractChat(body)
	case strings.HasSuffix(p, "/completions"):
		r = extractField(body, "prompt", "completions")
	case strings.HasSuffix(p, "/embeddings"):
		r = extractField(body, "input", "embeddings")
	case strings.HasSuffix(p, "/moderations"):
		r = extractField(body, "input", "moderations")
	case strings.HasSuffix(p, "/rerank") || strings.HasSuffix(p, "/reranker"):
		r = extractRerank(body)
	case strings.HasSuffix(p, "/messages"):
		r = extractAnthropic(body)
	case strings.HasSuffix(p, "/responses"):
		r = extractResponses(body)
	case strings.Contains(p, "/images/"):
		r = extractField(body, "prompt", "images")
	case strings.Contains(p, ":generatecontent"), strings.Contains(p, ":streamgeneratecontent"):
		r = extractGemini(body)
	default:
		r = extractGeneric(body)
	}
	r.Model = model
	return r
}

// Output extracts the model-generated text from an upstream response body,
// handling both JSON responses and SSE (text/event-stream) streams. It is used
// when response auditing is enabled.
func Output(contentType string, body []byte) Result {
	ct := strings.ToLower(contentType)
	var b strings.Builder
	if strings.Contains(ct, "text/event-stream") {
		for _, line := range strings.Split(string(body), "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "data:") {
				continue
			}
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data == "" || data == "[DONE]" {
				continue
			}
			var v interface{}
			if json.Unmarshal([]byte(data), &v) == nil {
				collectOutputText(v, &b)
			}
		}
	} else {
		var v interface{}
		if json.Unmarshal(body, &v) == nil {
			collectOutputText(v, &b)
		}
	}
	txt := strings.TrimSpace(b.String())
	return Result{Text: txt, Checkable: txt != "", Kind: "response"}
}

// outputKeys are the JSON keys whose string values hold model-generated text
// across OpenAI (chat/completions/responses), Anthropic and Gemini shapes.
var outputKeys = map[string]bool{
	"content":           true,
	"text":              true,
	"output_text":       true,
	"reasoning_content": true,
	"refusal":           true,
}

// collectOutputText recursively appends string values found under outputKeys.
func collectOutputText(v interface{}, b *strings.Builder) {
	switch t := v.(type) {
	case map[string]interface{}:
		for k, val := range t {
			if s, ok := val.(string); ok && outputKeys[k] {
				appendLine(b, s)
				continue
			}
			collectOutputText(val, b)
		}
	case []interface{}:
		for _, e := range t {
			collectOutputText(e, b)
		}
	}
}

// Clamp trims text to at most max characters (rune-safe).
func Clamp(s string, max int) string {
	if max <= 0 || len(s) <= max {
		return s
	}
	// len is a byte cap; ensure we don't split a multi-byte rune.
	b := []byte(s)
	if len(b) <= max {
		return s
	}
	cut := max
	for cut > 0 && (b[cut]&0xC0) == 0x80 {
		cut--
	}
	return string(b[:cut])
}

func peekModel(body []byte) string {
	var m struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &m)
	return m.Model
}

// --- chat completions (OpenAI) ---

// roleContent is the common {role, content} message shape.
type roleContent struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// lastUserText returns the text of the most recent user-role message.
//
// Client tools (codex, IDE agents, etc.) inject a large benign system prompt
// plus the full conversation history on every call. Concatenating all of that
// and sending it to JEV dilutes the safety signal — a single harmful line
// buried in thousands of benign tokens scores as "safe". Evaluating only the
// newest user turn keeps the check sharp, cheap, and matches exactly what the
// caller just submitted.
func lastUserText(items []roleContent) string {
	for i := len(items) - 1; i >= 0; i-- {
		if items[i].Role != "user" {
			continue
		}
		var b strings.Builder
		writePart(&b, items[i].Content)
		if t := strings.TrimSpace(b.String()); t != "" {
			return t
		}
	}
	return ""
}

func extractChat(body []byte) Result {
	var req struct {
		Messages []roleContent `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Messages) == 0 {
		return Result{Kind: "chat", Checkable: false}
	}
	txt := lastUserText(req.Messages)
	if txt == "" {
		// No user message; fall back to the last message's text.
		var b strings.Builder
		writePart(&b, req.Messages[len(req.Messages)-1].Content)
		txt = strings.TrimSpace(b.String())
	}
	return Result{Text: txt, Checkable: txt != "", Kind: "chat"}
}

// writePart appends text from a content value that may be a JSON string or an
// array of typed content parts ({"type":"text"|"input_text","text":"..."}).
func writePart(b *strings.Builder, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		appendLine(b, s)
		return
	}
	var parts []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if json.Unmarshal(raw, &parts) == nil {
		for _, p := range parts {
			if p.Text != "" {
				appendLine(b, p.Text)
			}
		}
	}
}

// --- Anthropic /v1/messages ---

func extractAnthropic(body []byte) Result {
	var req struct {
		Messages []roleContent `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Messages) == 0 {
		return Result{Kind: "anthropic", Checkable: false}
	}
	txt := lastUserText(req.Messages)
	if txt == "" {
		var b strings.Builder
		writePart(&b, req.Messages[len(req.Messages)-1].Content)
		txt = strings.TrimSpace(b.String())
	}
	return Result{Text: txt, Checkable: txt != "", Kind: "anthropic"}
}

// --- OpenAI Responses API (/v1/responses) ---

func extractResponses(body []byte) Result {
	// `instructions` here is the client tool's system prompt — intentionally
	// ignored so it doesn't dilute the safety signal.
	var req struct {
		Input json.RawMessage `json:"input"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Result{Kind: "responses", Checkable: false}
	}

	// input may be a bare string or an array of message-like items.
	var s string
	if json.Unmarshal(req.Input, &s) == nil {
		s = strings.TrimSpace(s)
		return Result{Text: s, Checkable: s != "", Kind: "responses"}
	}
	var items []roleContent
	if json.Unmarshal(req.Input, &items) == nil && len(items) > 0 {
		txt := lastUserText(items)
		if txt == "" {
			var b strings.Builder
			writePart(&b, items[len(items)-1].Content)
			txt = strings.TrimSpace(b.String())
		}
		return Result{Text: txt, Checkable: txt != "", Kind: "responses"}
	}
	return Result{Kind: "responses", Checkable: false}
}

// --- Gemini generateContent ---

func extractGemini(body []byte) Result {
	// systemInstruction is the client's system prompt — ignored on purpose.
	var req struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Result{Kind: "gemini", Checkable: false}
	}
	// Evaluate the most recent user turn only (see lastUserText rationale).
	for i := len(req.Contents) - 1; i >= 0; i-- {
		c := req.Contents[i]
		if c.Role != "" && c.Role != "user" {
			continue
		}
		var b strings.Builder
		for _, p := range c.Parts {
			appendLine(&b, p.Text)
		}
		if t := strings.TrimSpace(b.String()); t != "" {
			return Result{Text: t, Checkable: true, Kind: "gemini"}
		}
	}
	return Result{Kind: "gemini", Checkable: false}
}

// --- rerank ---

func extractRerank(body []byte) Result {
	var req struct {
		Query string `json:"query"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return Result{Kind: "rerank", Checkable: false}
	}
	txt := strings.TrimSpace(req.Query)
	return Result{Text: txt, Checkable: txt != "", Kind: "rerank"}
}

// --- single-field endpoints (prompt / input) ---

func extractField(body []byte, field, kind string) Result {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return Result{Kind: kind, Checkable: false}
	}
	raw, ok := m[field]
	if !ok {
		return Result{Kind: kind, Checkable: false}
	}
	var b strings.Builder
	collectStrings(raw, &b)
	txt := strings.TrimSpace(b.String())
	return Result{Text: txt, Checkable: txt != "", Kind: kind}
}

// --- generic fallback ---

func extractGeneric(body []byte) Result {
	var v interface{}
	if err := json.Unmarshal(body, &v); err != nil {
		return Result{Kind: "unknown", Checkable: false}
	}
	var b strings.Builder
	walk(v, &b)
	txt := strings.TrimSpace(b.String())
	return Result{Text: txt, Checkable: txt != "", Kind: "generic"}
}

// collectStrings appends every string leaf found in raw.
func collectStrings(raw json.RawMessage, b *strings.Builder) {
	var v interface{}
	if json.Unmarshal(raw, &v) != nil {
		return
	}
	walk(v, b)
}

// walk recursively appends every string leaf in v.
func walk(v interface{}, b *strings.Builder) {
	switch t := v.(type) {
	case string:
		appendLine(b, t)
	case []interface{}:
		for _, e := range t {
			walk(e, b)
		}
	case map[string]interface{}:
		for _, e := range t {
			walk(e, b)
		}
	}
}

func appendLine(b *strings.Builder, s string) {
	s = strings.TrimSpace(s)
	if s == "" {
		return
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	b.WriteString(s)
}
