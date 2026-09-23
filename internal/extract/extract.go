// Package extract pulls the user-provided input text out of an upstream API
// request body, based on the request path and content type. It covers the
// OpenAI endpoint surface (chat/completions, completions, responses,
// embeddings, moderations, images, audio, videos, realtime, assistants,
// threads, rerank) plus Anthropic's /v1/messages and Gemini's generateContent,
// and falls back to a generic string collector for anything unrecognized, so no
// endpoint is ever forwarded completely unchecked.
package extract

import (
	"bytes"
	"encoding/json"
	"io"
	"mime"
	"mime/multipart"
	"sort"
	"strings"
)

// Result is the outcome of extracting input from a request.
type Result struct {
	Text      string // concatenated user input to evaluate
	Checkable bool   // false => skip the JEV check (unknown/binary/empty)
	Kind      string // short label for logging
	Model     string // model name from the body, if present, for logging
}

// endpoint describes how one API endpoint carries user input.
type endpoint struct {
	kind  string
	match func(path string) bool
	// extract returns the text of a JSON body. A non-empty Kind overrides the
	// endpoint's label (used by the generic fallback). May be nil for endpoints
	// that carry no user text at all.
	extract func(body []byte) Result
	// noText marks an endpoint that provably carries no user input, so a miss
	// reads as "nothing to check" rather than "unknown endpoint".
	noText bool
}

// endpoints is the dispatch table. Order matters: the first match wins, so
// narrower paths come first (e.g. /threads/{id}/messages before /messages).
var endpoints = []endpoint{
	// --- OpenAI ---
	{kind: "chat", match: suffix("/chat/completions"), extract: textFromMessages},
	{kind: "completions", match: suffix("/completions"), extract: fieldText("prompt")},
	{kind: "responses", match: suffix("/responses"), extract: textFromResponses},
	{kind: "embeddings", match: suffix("/embeddings"), extract: fieldText("input")},
	{kind: "moderations", match: suffix("/moderations"), extract: fieldText("input")},
	{kind: "images", match: contains("/images/"), extract: fieldText("prompt")},
	{kind: "audio-speech", match: suffix("/audio/speech"), extract: fieldText("input")},
	{kind: "videos", match: videoPath, extract: fieldText("prompt")},
	{kind: "realtime", match: suffix("/realtime/sessions"), extract: fieldText("instructions")},
	{kind: "assistants", match: suffix("/assistants"), extract: firstFieldText("instructions", "description")},
	{kind: "thread-messages", match: threadsMessages, extract: fieldText("content")},
	{kind: "thread-runs", match: threadsRuns, extract: firstFieldText("additional_instructions", "instructions")},
	{kind: "rerank", match: rerankPath, extract: fieldText("query")},

	// --- Anthropic / Gemini ---
	// count_tokens carries the same messages/contents payload as the generation
	// calls, so it is routed through the same newest-turn extractors. Falling
	// through to genericText would concatenate the whole conversation, and the
	// benign system prompt plus full history would dilute a harmful newest turn
	// until it scored as safe.
	{kind: "anthropic", match: suffix("/messages/count_tokens"), extract: textFromMessages},
	{kind: "anthropic", match: suffix("/messages"), extract: textFromMessages},
	{kind: "gemini", match: contains(":generatecontent"), extract: textFromGemini},
	{kind: "gemini", match: contains(":streamgeneratecontent"), extract: textFromGemini},
	{kind: "gemini", match: contains(":counttokens"), extract: textFromGemini},

	// --- multipart endpoints (audio transcriptions and translations, image
	// edits, file uploads) are matched by content type in Extract before this
	// table is consulted, so they must NOT appear here: an entry would be
	// unreachable, and marking one noText would silently stop checking the
	// prompt field those forms do carry. ---

	// --- endpoints that carry no user-supplied text ---
	{kind: "files", match: suffix("/files"), noText: true},
	{kind: "uploads", match: suffix("/uploads"), noText: true},
	{kind: "batches", match: suffix("/batches"), noText: true},
	{kind: "fine-tuning", match: contains("/fine_tuning/"), noText: true},
	{kind: "vector-stores", match: contains("/vector_stores"), noText: true},
	{kind: "models", match: suffix("/models"), noText: true},
}

// genericEndpoint handles unrecognized paths by collecting every string leaf.
var genericEndpoint = endpoint{kind: "generic", extract: genericText}

// Extract picks the user-input text from body based on the request path and
// content type.
func Extract(path, contentType string, body []byte) Result {
	// Form bodies are not JSON, so they are matched by content type before the
	// path table: audio transcriptions, image edits and file uploads all carry
	// their prompt in a form field.
	if strings.HasPrefix(strings.ToLower(contentType), "multipart/form-data") {
		r := extractForm(contentType, body)
		r.Text = strings.TrimSpace(r.Text)
		r.Checkable = r.Text != ""
		return r
	}

	ep := lookup(strings.ToLower(path))
	r := Result{}
	if ep.extract != nil {
		r = ep.extract(body)
	}
	if r.Kind == "" {
		r.Kind = ep.kind
	}
	if ep.noText {
		r.Text, r.Checkable = "", false
	} else {
		r.Text = strings.TrimSpace(r.Text)
		r.Checkable = r.Text != ""
	}
	if r.Model == "" {
		r.Model = peekModel(body)
	}
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
// `delta` is the streaming key of the Responses API (response.output_text.delta,
// response.reasoning_summary_text.delta) and of Anthropic's content_block_delta;
// without it a streamed response would audit as empty.
var outputKeys = map[string]bool{
	"content":           true,
	"text":              true,
	"delta":             true,
	"output_text":       true,
	"reasoning_content": true,
	"refusal":           true,
	"summary_text":      true,
	"revised_prompt":    true,
	"arguments":         true,
}

// collectOutputText recursively appends string values found under outputKeys.
// Like walk, it visits keys in sorted order so the audited text is reproducible
// across runs instead of following Go's randomized map iteration.
func collectOutputText(v interface{}, b *strings.Builder) {
	switch t := v.(type) {
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := t[k]
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

// --- dispatch helpers ---

// lookup returns the descriptor for a lowercased path, or the generic fallback.
func lookup(p string) endpoint {
	for _, ep := range endpoints {
		if ep.match(p) {
			return ep
		}
	}
	return genericEndpoint
}

func suffix(s string) func(string) bool {
	return func(p string) bool { return strings.HasSuffix(p, s) }
}

func contains(s string) func(string) bool {
	return func(p string) bool { return strings.Contains(p, s) }
}

// videoPath matches the video endpoints: POST /v1/videos and the per-video
// sub-resources such as /v1/videos/{id}/remix.
func videoPath(p string) bool {
	return strings.HasSuffix(p, "/videos") || strings.Contains(p, "/videos/")
}

func rerankPath(p string) bool {
	return strings.HasSuffix(p, "/rerank") ||
		strings.HasSuffix(p, "/rerankers") ||
		strings.HasSuffix(p, "/reranker")
}

func threadsMessages(p string) bool {
	return strings.Contains(p, "/threads/") && strings.HasSuffix(p, "/messages")
}

func threadsRuns(p string) bool {
	return strings.Contains(p, "/threads/") &&
		(strings.HasSuffix(p, "/runs") || strings.HasSuffix(p, "/runs/stream"))
}

func peekModel(body []byte) string {
	var m struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(body, &m)
	return m.Model
}

// --- message-shaped requests (chat completions, Anthropic messages) ---

// message is the common {role, content} shape shared by OpenAI chat and
// Anthropic messages.
type message struct {
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
}

// textFromMessages extracts the newest client-supplied turn from a
// message-array request. `system` / `instructions` are the client's system
// prompt and are ignored on purpose (see newestTurn).
func textFromMessages(body []byte) Result {
	var req struct {
		Messages []message `json:"messages"`
	}
	if json.Unmarshal(body, &req) != nil || len(req.Messages) == 0 {
		return Result{}
	}
	return Result{Text: newestTurn(req.Messages)}
}

// newestTurn returns the text of the newest client-supplied turn: the latest
// user message, or a tool result that arrived after it.
//
// Client tools (codex, IDE agents, etc.) inject a large benign system prompt
// plus the full conversation history on every call, and only the newest turn is
// new information. Concatenating all of it and sending it to JEV dilutes the
// safety signal — a single harmful line buried in thousands of benign tokens
// scores as "safe" — while the newest turn is exactly what the caller just
// submitted. Tool results (OpenAI role "tool", Anthropic tool_result parts)
// count because in an agent loop the newest submission is often a tool's output,
// which is itself attacker-reachable content.
func newestTurn(msgs []message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		switch msgs[i].Role {
		case "user", "tool":
			if t := contentText(msgs[i].Content); t != "" {
				return t
			}
		}
	}
	// No user/tool turn: fall back to the newest message carrying any text, so
	// the request is not forwarded completely unchecked.
	for i := len(msgs) - 1; i >= 0; i-- {
		if t := contentText(msgs[i].Content); t != "" {
			return t
		}
	}
	return ""
}

// contentText renders a content-shaped value as trimmed text.
func contentText(raw json.RawMessage) string {
	var b strings.Builder
	writeContent(&b, raw)
	return strings.TrimSpace(b.String())
}

// writeContent appends the text carried by a content-shaped value: a bare
// string, an array of typed parts, or an object nesting its text under `text`
// (normal parts), `content` (Anthropic tool_result) or `output` (Responses
// function_call_output). Encoding-only parts such as image_url carry none of
// those keys and are skipped, so base64 blobs never reach JEV.
func writeContent(b *strings.Builder, raw json.RawMessage) {
	if len(raw) == 0 {
		return
	}
	var s string
	if json.Unmarshal(raw, &s) == nil {
		appendLine(b, s)
		return
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) == nil {
		for _, p := range parts {
			writeContent(b, p)
		}
		return
	}
	var obj map[string]json.RawMessage
	if json.Unmarshal(raw, &obj) != nil {
		return
	}
	for _, key := range []string{"text", "content", "output"} {
		if v, ok := obj[key]; ok {
			writeContent(b, v)
		}
	}
}

// --- single-field requests ---

// fieldText extracts one named field (a string, or the text parts inside it).
func fieldText(field string) func([]byte) Result {
	return func(body []byte) Result {
		var m map[string]json.RawMessage
		if json.Unmarshal(body, &m) != nil {
			return Result{}
		}
		raw, ok := m[field]
		if !ok {
			return Result{}
		}
		return Result{Text: contentText(raw)}
	}
}

// firstFieldText returns the first of the named fields that carries text.
func firstFieldText(fields ...string) func([]byte) Result {
	return func(body []byte) Result {
		var m map[string]json.RawMessage
		if json.Unmarshal(body, &m) != nil {
			return Result{}
		}
		for _, f := range fields {
			raw, ok := m[f]
			if !ok {
				continue
			}
			if t := contentText(raw); t != "" {
				return Result{Text: t}
			}
		}
		return Result{}
	}
}

// --- OpenAI Responses API (/v1/responses) ---

// responsesItem is one element of a Responses `input` array: either a message
// (role + content) or tool traffic (function_call_output).
type responsesItem struct {
	Type    string          `json:"type"`
	Role    string          `json:"role"`
	Content json.RawMessage `json:"content"`
	Output  json.RawMessage `json:"output"`
}

// textFromResponses extracts the newest client-supplied turn from a Responses
// request. `instructions` is the client's system prompt — intentionally ignored
// so it does not dilute the safety signal (see newestTurn).
func textFromResponses(body []byte) Result {
	var req struct {
		Input json.RawMessage `json:"input"`
	}
	if json.Unmarshal(body, &req) != nil {
		return Result{}
	}

	// input may be a bare string or an array of message-like items.
	var s string
	if json.Unmarshal(req.Input, &s) == nil {
		return Result{Text: strings.TrimSpace(s)}
	}
	var items []responsesItem
	if json.Unmarshal(req.Input, &items) != nil {
		return Result{}
	}

	for i := len(items) - 1; i >= 0; i-- {
		it := items[i]
		if it.Type == "function_call_output" || it.Type == "custom_tool_call_output" {
			if t := contentText(it.Output); t != "" {
				return Result{Text: t}
			}
			continue
		}
		if it.Role == "user" || it.Role == "tool" {
			if t := contentText(it.Content); t != "" {
				return Result{Text: t}
			}
		}
	}
	// No user/tool item: fall back to the newest item carrying any text.
	for i := len(items) - 1; i >= 0; i-- {
		if t := contentText(items[i].Content); t != "" {
			return Result{Text: t}
		}
		if t := contentText(items[i].Output); t != "" {
			return Result{Text: t}
		}
	}
	return Result{}
}

// --- Gemini generateContent ---

func textFromGemini(body []byte) Result {
	// systemInstruction is the client's system prompt — ignored on purpose.
	var req struct {
		Contents []struct {
			Role  string `json:"role"`
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"contents"`
	}
	if json.Unmarshal(body, &req) != nil {
		return Result{}
	}
	// Evaluate the most recent user turn only (see newestTurn rationale).
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
			return Result{Text: t}
		}
	}
	return Result{}
}

// --- multipart/form-data ---

// formTextFields are the multipart field names that carry user-supplied text
// across the form-accepting endpoints (audio transcriptions and translations,
// image edits, file uploads).
var formTextFields = map[string]bool{
	"prompt":       true,
	"input":        true,
	"instructions": true,
	"text":         true,
	"query":        true,
	"content":      true,
	"message":      true,
}

// maxFormField caps how much of a single form field is collected; the body as a
// whole is already bounded by the proxy's inspect cap.
const maxFormField = 256 << 10

// extractForm pulls the text fields out of a multipart/form-data body. File
// parts (binary uploads) are skipped: only the accompanying text is checkable.
func extractForm(contentType string, body []byte) Result {
	_, params, err := mime.ParseMediaType(contentType)
	if err != nil || params["boundary"] == "" {
		return Result{Kind: "form"}
	}

	var (
		b     strings.Builder
		model string
		mr    = multipart.NewReader(bytes.NewReader(body), params["boundary"])
	)
	for {
		part, err := mr.NextPart()
		if err != nil {
			break // io.EOF, or a malformed body
		}
		name := part.FormName()
		if part.FileName() != "" { // binary upload, not text
			continue
		}
		switch {
		case name == "model":
			model = readField(part)
		case formTextFields[name]:
			appendLine(&b, readField(part))
		}
	}
	return Result{Text: strings.TrimSpace(b.String()), Kind: "form", Model: model}
}

func readField(part *multipart.Part) string {
	v, _ := io.ReadAll(io.LimitReader(part, maxFormField))
	return string(v)
}

// --- generic fallback ---

// genericText collects every string leaf of an unrecognized body so an unknown
// endpoint is still checked. Non-JSON bodies yield nothing to check.
func genericText(body []byte) Result {
	var v interface{}
	if json.Unmarshal(body, &v) != nil {
		return Result{Kind: "unknown"}
	}
	var b strings.Builder
	walk(v, &b)
	return Result{Text: strings.TrimSpace(b.String()), Kind: "generic"}
}

// walk recursively appends every string leaf in v, skipping embedded data URIs
// (base64 images and audio) which would otherwise swamp the text sent to JEV.
//
// Object keys are visited in sorted order. Go randomizes map iteration, and the
// collected text is both the safety signal and the dedup cache key: an unstable
// order would make one body hash differently on every call, so the score cache
// could never hit on exactly the unknown endpoints that reach genericText.
func walk(v interface{}, b *strings.Builder) {
	switch t := v.(type) {
	case string:
		if strings.HasPrefix(strings.TrimSpace(t), "data:") {
			return
		}
		appendLine(b, t)
	case []interface{}:
		for _, e := range t {
			walk(e, b)
		}
	case map[string]interface{}:
		keys := make([]string, 0, len(t))
		for k := range t {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			walk(t[k], b)
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
