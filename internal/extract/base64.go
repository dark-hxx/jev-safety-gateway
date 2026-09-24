package extract

import (
	"encoding/base64"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A client can hand the model its payload base64-encoded. Claude Code's
// session-title prompt does exactly that — it wraps the session content in
// <session>BASE64</session> — and the same prompt scored 0.15 ("harmful") as
// plain text and 0.80 ("safe") encoded, so base64 was a free bypass: the
// gateway evaluated the encoded instruction around the blob plus the gibberish
// itself, and the harmful content never appeared in the text it scored.
//
// Expanding the runs in place makes both encodings of one payload produce the
// same submission, hence the same verdict and the same dedup cache key. The
// pass is a single scan with a lookup table that allocates nothing unless a run
// actually decodes (measured ~1.5 GB/s, i.e. microseconds for the sizes that
// reach JEV, against a JEV round trip of ~300 ms), and decoding only ever
// shortens the text — base64 is four characters per three bytes — so it cannot
// push the user's own text out of max_state_chars.
const (
	// minBase64Run is the shortest run of base64 characters worth decoding.
	// Ordinary text has a space or punctuation every few characters, so a run
	// this long is already unusual, and a 24-byte decode of a non-base64 token
	// (a hash, an id) has to be 90% printable to qualify — which random bytes
	// essentially never are.
	minBase64Run = 32
	// maxBase64Run caps how much of one encoded run is decoded. Anything past
	// it is beyond what the proxy's own clamp would keep anyway.
	maxBase64Run = 64 << 10
	// maxBase64Decoded caps the total decoded text produced per pass.
	maxBase64Decoded = 64 << 10
	// maxBase64Segments caps how many runs one pass decodes.
	maxBase64Segments = 8
	// maxBase64Passes bounds the repeat expansion. Each pass only ever
	// shortens the text, so the work is bounded by the first pass's input;
	// repeating reaches a payload that was encoded twice.
	maxBase64Passes = 3
)

// b64Byte is true for the standard base64 alphabet plus '=' padding.
var b64Byte = func() (t [256]bool) {
	for c := 'A'; c <= 'Z'; c++ {
		t[c] = true
	}
	for c := 'a'; c <= 'z'; c++ {
		t[c] = true
	}
	for c := '0'; c <= '9'; c++ {
		t[c] = true
	}
	t['+'], t['/'], t['='] = true, true, true
	return
}()

// ExpandBase64 replaces long base64 runs in text with the text they encode,
// when they encode plausible text at all. Runs that do not decode — hashes,
// ids, image and audio data — are left exactly as they were, so the text sent
// to JEV is never mangled into something else.
//
// Base64url (`-`/`_`) and unpadded variants are deliberately not matched:
// guessing an offset for an unpadded run decodes garbage, and a JWT or a UUID
// that happens to use those characters is not a payload worth decoding.
func ExpandBase64(text string) string {
	for i := 0; i < maxBase64Passes; i++ {
		next := expandBase64Once(text)
		if next == text {
			break
		}
		text = next
	}
	return text
}

// expandBase64Once is one left-to-right pass: every maximal run of base64
// characters is tested, and a run that decodes to text is replaced by it.
func expandBase64Once(text string) string {
	var (
		out       strings.Builder
		decoded   int
		segments  int
		lastWrite int
	)
	for i := 0; i < len(text); {
		if !b64Byte[text[i]] {
			i++
			continue
		}
		j := i
		for j < len(text) && b64Byte[text[j]] {
			j++
		}
		if len(text[i:j]) >= minBase64Run && segments < maxBase64Segments && decoded < maxBase64Decoded {
			if dec, ok := decodeText(text[i:j]); ok {
				if out.Len() == 0 {
					out.Grow(len(text))
				}
				out.WriteString(text[lastWrite:i])
				out.WriteString(dec)
				lastWrite = j
				decoded += len(dec)
				segments++
			}
		}
		i = j
	}
	if lastWrite == 0 {
		return text // nothing decoded: return the original string, no copy
	}
	out.WriteString(text[lastWrite:])
	return out.String()
}

// decodeText decodes one candidate run, reporting ok=false unless the result is
// plausible text.
func decodeText(run string) (string, bool) {
	if len(run) > maxBase64Run {
		run = run[:maxBase64Run]
	}
	// Standard base64 is padded to a multiple of four. A run that is not is
	// either an unpadded variant or ordinary text that happens to use the
	// alphabet, and decoding it at the wrong offset would produce garbage that
	// looks like a payload.
	if len(run)%4 != 0 {
		return "", false
	}
	raw, err := base64.StdEncoding.DecodeString(run)
	if err != nil || !utf8.Valid(raw) {
		return "", false
	}
	if !looksLikeText(raw) {
		return "", false
	}
	return string(raw), true
}

// looksLikeText reports whether b reads as human text: almost entirely
// printable and carrying at least one letter. It is what separates a decoded
// payload from the byte soup that a hash, an id or a binary blob decodes to.
func looksLikeText(b []byte) bool {
	total, printable, letters := 0, 0, 0
	for _, r := range string(b) {
		total++
		switch {
		case r == '\n' || r == '\r' || r == '\t':
			printable++
		case unicode.IsPrint(r):
			printable++
			if unicode.IsLetter(r) {
				letters++
			}
		}
	}
	if total == 0 || letters == 0 {
		return false
	}
	return printable*10 >= total*9
}
