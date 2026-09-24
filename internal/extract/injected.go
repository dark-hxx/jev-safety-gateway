package extract

import "strings"

// Clients inject their own context into the newest user message as
// <system-reminder> blocks: the CLAUDE.md dump, the memory index, the git
// status, tool reminders. Those blocks are client-injected context, exactly
// like the `system` field the message extractors already ignore on purpose —
// and they are large. A first turn in this repository carries ~24 KB of them,
// more than the proxy's max_state_chars (16 KB), so the clamp kept the
// boilerplate and cut the user's own prompt off the end: JEV scored the
// client's context and never saw the input it was asked about. Every new
// session's first turn was blocked at ~0.10 because the rules text itself reads
// as risky to the safety model.
//
// The trade-off is that content hidden inside the tags is no longer evaluated.
// That is the same trade-off the extractors already make for `system` /
// `instructions`, and it does not widen the hole: a payload parked in `system`
// is unchecked either way. A turn that held nothing but client context is left
// with no text at all, which reads as "nothing to check" and is logged as such
// rather than being submitted.
const (
	reminderOpen  = "<system-reminder>"
	reminderClose = "</system-reminder>"
)

// StripInjected removes harness-injected <system-reminder> blocks from s. An
// unterminated opening tag drops the rest of the text, which is what a body
// truncated at the proxy's inspect cap looks like.
func StripInjected(s string) string {
	if indexFold(s, reminderOpen, 0) < 0 {
		return s
	}
	var out strings.Builder
	for {
		i := indexFold(s, reminderOpen, 0)
		if i < 0 {
			out.WriteString(s)
			break
		}
		out.WriteString(s[:i])
		s = s[i+len(reminderOpen):]
		j := indexFold(s, reminderClose, 0)
		if j < 0 {
			break // unterminated: the block runs to the end of the text
		}
		s = s[j+len(reminderClose):]
	}
	return strings.TrimSpace(out.String())
}

// indexFold is strings.Index with ASCII case folding. It exists so the reminder
// tags can be matched without allocating a lowercased copy of the whole
// submission, and it is O(len(s)) in practice because the first byte of a tag
// (`<`) is rare in ordinary text.
func indexFold(s, sub string, from int) int {
	if sub == "" {
		return from
	}
	first := lowerASCII(sub[0])
	for i := from; i+len(sub) <= len(s); i++ {
		if lowerASCII(s[i]) != first {
			continue
		}
		for j := 1; j < len(sub); j++ {
			if lowerASCII(s[i+j]) != lowerASCII(sub[j]) {
				goto next
			}
		}
		return i
	next:
	}
	return -1
}

func lowerASCII(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
