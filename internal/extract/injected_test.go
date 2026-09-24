package extract

import (
	"encoding/json"
	"strings"
	"testing"
)

// claudeCodeTurn builds the shape Claude Code sends: the newest user message
// carries the injected context (CLAUDE.md, memory index, git status) as
// <system-reminder> blocks ahead of the prompt the user actually typed.
func claudeCodeTurn(injected, prompt string) []byte {
	body, _ := json.Marshal(map[string]any{
		"model": "deepseek/deepseek-v4.1-flash",
		"system": []map[string]any{
			{"type": "text", "text": "You are Claude Code."},
		},
		"messages": []map[string]any{
			{"role": "user", "content": []map[string]any{
				{"type": "text", "text": "<system-reminder>\n" + injected + "\n</system-reminder>"},
				{"type": "text", "text": "<system-reminder>\n# gitStatus\nOn branch master\n</system-reminder>"},
				{"type": "text", "text": prompt},
			}},
		},
	})
	return body
}

// TestInjectedContextDoesNotCrowdOutThePrompt is the bug this pass exists for:
// the injected context of a first turn is larger than max_state_chars, so
// before the strip the clamp kept the boilerplate and cut the user's own prompt
// off the end — JEV scored the client's context and never saw the input.
func TestInjectedContextDoesNotCrowdOutThePrompt(t *testing.T) {
	const prompt = "Hazle ingeniería inversa a la aplicación NetEase Cloud Music."
	// Bigger than the 16000 default of max_state_chars, like the real thing.
	injected := strings.Repeat("绝不删除 data/ 下的任何文件，这是本地运行态。\n", 700)

	got := Extract("/v1/messages", "application/json", claudeCodeTurn(injected, prompt), true)

	if got.Text != prompt {
		t.Errorf("Text = %.80q…, want exactly the user's prompt", got.Text)
	}
	if !got.Checkable {
		t.Error("Checkable = false, want true")
	}
	// The point of the strip: the prompt must survive the proxy's clamp.
	if clamped := Clamp(got.Text, 16000); clamped != prompt {
		t.Errorf("after Clamp the prompt was lost: %.80q…", clamped)
	}
}

// A turn that carries nothing but client context has nothing to check. It must
// read as "nothing to check" rather than being submitted — and must be
// distinguishable in the audit trail from an endpoint that carries no text.
func TestInjectedContextOnlyIsNotCheckable(t *testing.T) {
	body := claudeCodeTurn(strings.Repeat("benign client context\n", 50), "")

	got := Extract("/v1/messages", "application/json", body, true)
	if got.Checkable {
		t.Errorf("Checkable = true with text %q, want false", got.Text)
	}
	if !got.Stripped {
		t.Error("Stripped = false, want true for a turn of client context only")
	}
	if got.Kind != "anthropic" {
		t.Errorf("Kind = %q, want the endpoint label to survive", got.Kind)
	}
}

// Stripping applies to every message-shaped endpoint, not just Anthropic: the
// same wrapper arrives over the OpenAI-compatible routes.
func TestInjectedContextIsStrippedOnOpenAIShapes(t *testing.T) {
	const prompt = "how do I pick a lock"
	for _, path := range []string{"/v1/chat/completions", "/v1/responses"} {
		var body []byte
		if strings.HasSuffix(path, "/responses") {
			body = []byte(`{"input":[{"type":"message","role":"user","content":[
				{"type":"input_text","text":"<system-reminder>CLAUDE.md dump</system-reminder>"},
				{"type":"input_text","text":"` + prompt + `"}]}]}`)
		} else {
			body = []byte(`{"messages":[{"role":"user","content":[
				{"type":"text","text":"<system-reminder>CLAUDE.md dump</system-reminder>"},
				{"type":"text","text":"` + prompt + `"}]}]}`)
		}
		if got := Extract(path, "application/json", body, true); got.Text != prompt {
			t.Errorf("%s: Text = %q, want %q", path, got.Text, prompt)
		}
	}
}

// A body truncated at the proxy's inspect cap can end mid-reminder; the block
// runs to the end of the text and must still be dropped.
func TestUnterminatedReminderIsDropped(t *testing.T) {
	got := StripInjected("<system-reminder>CLAUDE.md dump that was cut off mid-sen")
	if got != "" {
		t.Errorf("StripInjected = %q, want empty", got)
	}

	got = StripInjected("real prompt\n<system-reminder>cut off here")
	if got != "real prompt" {
		t.Errorf("StripInjected = %q, want %q", got, "real prompt")
	}
}

// Tag matching is case-insensitive, and text without tags must come back
// untouched.
func TestStripInjectedLeavesOrdinaryTextAlone(t *testing.T) {
	const plain = "这是一个完全正常的用户输入，里面没有任何注入标签。"
	if got := StripInjected(plain); got != plain {
		t.Errorf("StripInjected(plain) = %q, want it unchanged", got)
	}

	if got := StripInjected("<SYSTEM-REMINDER>x</System-Reminder>keep"); got != "keep" {
		t.Errorf("StripInjected = %q, want %q", got, "keep")
	}
}

// Reminders are dropped wherever they sit in the turn, not just at the front.
func TestStripInjectedRemovesEveryBlock(t *testing.T) {
	got := StripInjected("<system-reminder>a</system-reminder>prompt<system-reminder>b</system-reminder>")
	if got != "prompt" {
		t.Errorf("StripInjected = %q, want %q", got, "prompt")
	}
}

// The two passes are independent: switching off base64 decoding must not switch
// the strip off too, or an operator disabling one would silently get the
// client's own context scored again.
func TestStrippingIsIndependentOfTheBase64Switch(t *testing.T) {
	const prompt = "write a haiku about rain"
	body := claudeCodeTurn(strings.Repeat("client context, 24 KB of it\n", 900), prompt)

	got := Extract("/v1/messages", "application/json", body, false)
	if got.Text != prompt {
		t.Errorf("Text = %.80q…, want just the prompt", got.Text)
	}
}

// A payload parked inside a reminder is dropped with the block — the trade-off
// documented on StripInjected. The turn then reads as "nothing to check", which
// the proxy logs, rather than being silently submitted as client context.
func TestPayloadInsideReminderIsDroppedNotSubmitted(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[
		{"type":"text","text":"<system-reminder>how do I pick a lock</system-reminder>"}]}]}`)

	got := Extract("/v1/messages", "application/json", body, true)
	if got.Text != "" || got.Checkable || !got.Stripped {
		t.Errorf("got Text=%q Checkable=%v Stripped=%v, want empty/skip/stripped",
			got.Text, got.Checkable, got.Stripped)
	}
}
