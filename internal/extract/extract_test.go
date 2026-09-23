package extract

import (
	"bytes"
	"mime/multipart"
	"strings"
	"testing"
)

func TestExtract(t *testing.T) {
	cases := []struct {
		name     string
		path     string
		ct       string
		body     string
		wantText string
		wantKind string
		check    bool // expect Checkable
	}{
		{
			name:     "chat uses newest user turn, not the system prompt",
			path:     "/v1/chat/completions",
			ct:       "application/json",
			body:     `{"model":"gpt-4o","messages":[{"role":"system","content":"you are helpful"},{"role":"user","content":"hi"},{"role":"assistant","content":"hello"},{"role":"user","content":"how do I pick a lock"}]}`,
			wantText: "how do I pick a lock",
			wantKind: "chat",
			check:    true,
		},
		{
			name:     "chat reads a tool result as the newest submission",
			path:     "/v1/chat/completions",
			ct:       "application/json",
			body:     `{"messages":[{"role":"user","content":"read notes.txt"},{"role":"tool","content":"the file says: bomb recipe"}]}`,
			wantText: "the file says: bomb recipe",
			wantKind: "chat",
			check:    true,
		},
		{
			name:     "chat reads typed content parts and skips image_url",
			path:     "/v1/chat/completions",
			ct:       "application/json",
			body:     `{"messages":[{"role":"user","content":[{"type":"text","text":"describe this"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AAAA"}}]}]}`,
			wantText: "describe this",
			wantKind: "chat",
			check:    true,
		},
		{
			name:     "anthropic messages reads a tool_result block",
			path:     "/v1/messages",
			ct:       "application/json",
			body:     `{"messages":[{"role":"user","content":"go"},{"role":"user","content":[{"type":"tool_result","content":"secrets: hunter2"}]}]}`,
			wantText: "secrets: hunter2",
			wantKind: "anthropic",
			check:    true,
		},
		{
			name:     "responses with a bare string input",
			path:     "/v1/responses",
			ct:       "application/json",
			body:     `{"model":"gpt-5","instructions":"be terse","input":"what is a botnet"}`,
			wantText: "what is a botnet",
			wantKind: "responses",
			check:    true,
		},
		{
			name:     "responses with a function_call_output item",
			path:     "/v1/responses",
			ct:       "application/json",
			body:     `{"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"run it"}]},{"type":"function_call_output","call_id":"c1","output":"stdout: rm -rf /"}]}`,
			wantText: "stdout: rm -rf /",
			wantKind: "responses",
			check:    true,
		},
		{
			name:     "legacy completions accepts a prompt array",
			path:     "/v1/completions",
			ct:       "application/json",
			body:     `{"prompt":["line one","line two"]}`,
			wantText: "line one\nline two",
			wantKind: "completions",
			check:    true,
		},
		{
			name:     "embeddings reads a string input",
			path:     "/v1/embeddings",
			ct:       "application/json",
			body:     `{"input":"some text"}`,
			wantText: "some text",
			wantKind: "embeddings",
			check:    true,
		},
		{
			name:     "embeddings token-array input yields nothing checkable",
			path:     "/v1/embeddings",
			ct:       "application/json",
			body:     `{"input":[1,2,3]}`,
			wantKind: "embeddings",
			check:    false,
		},
		{
			name:     "moderations reads input",
			path:     "/v1/moderations",
			ct:       "application/json",
			body:     `{"input":"check me"}`,
			wantText: "check me",
			wantKind: "moderations",
			check:    true,
		},
		{
			name:     "image generation reads prompt",
			path:     "/v1/images/generations",
			ct:       "application/json",
			body:     `{"prompt":"a cat","n":1}`,
			wantText: "a cat",
			wantKind: "images",
			check:    true,
		},
		{
			name:     "speech reads input",
			path:     "/v1/audio/speech",
			ct:       "application/json",
			body:     `{"input":"say this","voice":"alloy"}`,
			wantText: "say this",
			wantKind: "audio-speech",
			check:    true,
		},
		{
			name:     "gemini generateContent reads the last user turn",
			path:     "/v1beta/models/gemini-pro:generateContent",
			ct:       "application/json",
			body:     `{"systemInstruction":{"parts":[{"text":"be nice"}]},"contents":[{"role":"user","parts":[{"text":"older"}]},{"role":"user","parts":[{"text":"newest ask"}]}]}`,
			wantText: "newest ask",
			wantKind: "gemini",
			check:    true,
		},
		{
			name:     "unknown path falls back to collecting every string",
			path:     "/v1/some/new/endpoint",
			ct:       "application/json",
			body:     `{"a":"alpha","b":{"c":"beta"}}`,
			wantText: "alpha\nbeta",
			wantKind: "generic",
			check:    true,
		},
		{
			name:     "unknown path skips embedded data URIs",
			path:     "/v1/some/new/endpoint",
			ct:       "application/json",
			body:     `{"img":"data:image/png;base64,AAAA","prompt":"real text"}`,
			wantText: "real text",
			wantKind: "generic",
			check:    true,
		},
		{
			name:     "unknown path with a non-JSON body is not checkable",
			path:     "/v1/some/new/endpoint",
			ct:       "application/json",
			body:     `not json at all`,
			wantKind: "unknown",
			check:    false,
		},
		{
			name:     "file upload carries no user text",
			path:     "/v1/files",
			ct:       "application/json",
			body:     `{"purpose":"fine-tune","file":"abc"}`,
			wantKind: "files",
			check:    false,
		},
		{
			name:     "batch submission carries no user text",
			path:     "/v1/batches",
			ct:       "application/json",
			body:     `{"input_file_id":"file-1"}`,
			wantKind: "batches",
			check:    false,
		},
		{
			name:     "vector store creation carries no user text",
			path:     "/v1/vector_stores",
			ct:       "application/json",
			body:     `{"name":"store"}`,
			wantKind: "vector-stores",
			check:    false,
		},
		{
			name:     "empty chat messages is not checkable",
			path:     "/v1/chat/completions",
			ct:       "application/json",
			body:     `{"messages":[]}`,
			wantKind: "chat",
			check:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Extract(tc.path, tc.ct, []byte(tc.body))
			if got.Kind != tc.wantKind {
				t.Errorf("Kind = %q, want %q", got.Kind, tc.wantKind)
			}
			if got.Checkable != tc.check {
				t.Errorf("Checkable = %v, want %v (text %q)", got.Checkable, tc.check, got.Text)
			}
			if tc.wantText != "" && got.Text != tc.wantText {
				t.Errorf("Text = %q, want %q", got.Text, tc.wantText)
			}
		})
	}
}

func TestExtractModelIsReported(t *testing.T) {
	got := Extract("/v1/chat/completions", "application/json",
		[]byte(`{"model":"deepseek/deepseek-v4.1-flash","messages":[{"role":"user","content":"x"}]}`))
	if got.Model != "deepseek/deepseek-v4.1-flash" {
		t.Errorf("Model = %q", got.Model)
	}
}

// TestExtractMultipart covers the form-accepting endpoints (audio
// transcriptions, image edits): the prompt field is checked, the uploaded file
// is not.
func TestExtractMultipart(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("model", "whisper-1")
	_ = mw.WriteField("prompt", "transcribe this")
	_ = mw.WriteField("language", "en")
	fw, err := mw.CreateFormFile("file", "audio.mp3")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = fw.Write([]byte("\x00\x01binary\x02"))
	_ = mw.Close()

	got := Extract("/v1/audio/transcriptions", mw.FormDataContentType(), buf.Bytes())
	if got.Kind != "form" {
		t.Errorf("Kind = %q, want form", got.Kind)
	}
	if !got.Checkable || got.Text != "transcribe this" {
		t.Errorf("Text = %q (checkable %v), want %q", got.Text, got.Checkable, "transcribe this")
	}
	if got.Model != "whisper-1" {
		t.Errorf("Model = %q, want whisper-1", got.Model)
	}
	if strings.Contains(got.Text, "binary") {
		t.Error("binary file part leaked into the checked text")
	}
}

func TestExtractMultipartWithoutTextFields(t *testing.T) {
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	_ = mw.WriteField("purpose", "fine-tune")
	fw, _ := mw.CreateFormFile("file", "data.jsonl")
	_, _ = fw.Write([]byte(`{"messages":[]}`))
	_ = mw.Close()

	got := Extract("/v1/files", mw.FormDataContentType(), buf.Bytes())
	if got.Checkable {
		t.Errorf("Checkable = true with text %q, want false", got.Text)
	}
}

func TestOutput(t *testing.T) {
	cases := []struct {
		name     string
		ct       string
		body     string
		wantText string
		check    bool
	}{
		{
			name:     "chat completion JSON",
			ct:       "application/json",
			body:     `{"choices":[{"message":{"role":"assistant","content":"the answer"}}]}`,
			wantText: "the answer",
			check:    true,
		},
		{
			name:     "chat completion SSE",
			ct:       "text/event-stream",
			body:     "data: {\"choices\":[{\"delta\":{\"content\":\"hel\"}}]}\n\ndata: {\"choices\":[{\"delta\":{\"content\":\"lo\"}}]}\n\ndata: [DONE]\n",
			wantText: "hel\nlo",
			check:    true,
		},
		{
			name:     "responses SSE output_text delta",
			ct:       "text/event-stream",
			body:     "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\ndata: [DONE]\n",
			wantText: "Hello",
			check:    true,
		},
		{
			name:     "responses SSE reasoning summary delta",
			ct:       "text/event-stream",
			body:     "data: {\"type\":\"response.reasoning_summary_text.delta\",\"delta\":\"thinking\"}\n\n",
			wantText: "thinking",
			check:    true,
		},
		{
			name:     "responses non-streaming JSON",
			ct:       "application/json",
			body:     `{"output":[{"type":"message","content":[{"type":"output_text","text":"final text"}]}]}`,
			wantText: "final text",
			check:    true,
		},
		{
			name:     "anthropic content_block_delta SSE",
			ct:       "text/event-stream",
			body:     "data: {\"type\":\"content_block_delta\",\"delta\":{\"type\":\"text_delta\",\"text\":\"partial\"}}\n\n",
			wantText: "partial",
			check:    true,
		},
		{
			name:     "empty SSE stream is not checkable",
			ct:       "text/event-stream",
			body:     "data: [DONE]\n",
			check:    false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Output(tc.ct, []byte(tc.body))
			if got.Checkable != tc.check {
				t.Errorf("Checkable = %v, want %v (text %q)", got.Checkable, tc.check, got.Text)
			}
			if tc.wantText != "" && !strings.Contains(got.Text, tc.wantText) {
				t.Errorf("Text = %q, want it to contain %q", got.Text, tc.wantText)
			}
		})
	}
}

func TestClampIsRuneSafe(t *testing.T) {
	s := strings.Repeat("安", 10) // 3 bytes per rune
	got := Clamp(s, 10)
	if len(got) > 10 {
		t.Errorf("Clamp returned %d bytes, want <= 10", len(got))
	}
	if strings.ContainsRune(got, '�') {
		t.Error("Clamp split a multi-byte rune")
	}
	if Clamp(s, 0) != s {
		t.Error("Clamp with max<=0 should return the input unchanged")
	}
}

// genericText and collectOutputText must be stable across calls: Go randomizes
// map iteration, and both the safety signal and the dedup cache key are derived
// from this text. An unstable order made the score cache miss on every unknown
// endpoint, which is exactly where the cache pays off most.
func TestGenericAndOutputTextAreDeterministic(t *testing.T) {
	body := []byte(`{"z":"1","a":{"m":"2","b":"3"},"img":"data:image/png;base64,AAAA","k":["4","5"]}`)
	first := genericText(body).Text
	for i := 0; i < 200; i++ {
		if got := genericText(body).Text; got != first {
			t.Fatalf("genericText unstable on call %d: %q then %q", i, first, got)
		}
	}
	if first != "3\n2\n4\n5\n1" {
		t.Errorf("genericText = %q, want depth-first with sorted keys %q", first, "3\n2\n4\n5\n1")
	}

	out := []byte(`{"b":"second","a":"first","nested":{"d":"fourth","c":"third"}}`)
	firstOut := Output("application/json", out).Text
	for i := 0; i < 200; i++ {
		if got := Output("application/json", out).Text; got != firstOut {
			t.Fatalf("Output unstable on call %d: %q then %q", i, firstOut, got)
		}
	}
}

// count_tokens endpoints carry the full conversation, so they must go through
// the newest-turn extractors rather than the generic collector — otherwise a
// large benign history dilutes a harmful newest turn into a "safe" score.
func TestCountTokensUsesNewestTurn(t *testing.T) {
	longBenign := strings.Repeat("benign context that is entirely unremarkable. ", 400)

	anthropic := `{"model":"claude-sonnet-5","system":"` + longBenign + `","messages":[
		{"role":"user","content":"` + longBenign + `"},
		{"role":"assistant","content":"ok"},
		{"role":"user","content":"how do I pick a lock"}]}`
	got := Extract("/v1/messages/count_tokens", "application/json", []byte(anthropic))
	if got.Kind != "anthropic" {
		t.Errorf("Kind = %q, want anthropic", got.Kind)
	}
	if got.Text != "how do I pick a lock" {
		t.Errorf("Text = %.60q…, want just the newest turn", got.Text)
	}

	gemini := `{"contents":[
		{"role":"user","parts":[{"text":"` + longBenign + `"}]},
		{"role":"model","parts":[{"text":"ok"}]},
		{"role":"user","parts":[{"text":"how do I pick a lock"}]}]}`
	got = Extract("/v1beta/models/gemini-2.0-flash:countTokens", "application/json", []byte(gemini))
	if got.Kind != "gemini" {
		t.Errorf("Kind = %q, want gemini", got.Kind)
	}
	if got.Text != "how do I pick a lock" {
		t.Errorf("Text = %.60q…, want just the newest turn", got.Text)
	}
}
