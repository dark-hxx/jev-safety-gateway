package extract

import (
	"strings"
	"testing"
)

// TestKnownPath covers the path gate's notion of "known": the endpoints the
// dispatch table recognizes must all report known, and the shapes a port scanner
// guesses must not.
func TestKnownPath(t *testing.T) {
	known := []string{
		// The OpenAI surface the table enumerates.
		"/v1/chat/completions",
		"/v1/completions",
		"/v1/responses",
		"/v1/embeddings",
		"/v1/moderations",
		"/v1/images/generations",
		"/v1/audio/speech",
		"/v1/videos",
		"/v1/videos/abc/remix",
		"/v1/realtime/sessions",
		"/v1/assistants",
		"/v1/threads/t1/messages",
		"/v1/threads/t1/runs",
		"/v1/rerank",
		// Anthropic and Gemini.
		"/v1/messages",
		"/v1/messages/count_tokens",
		"/v1beta/models/gemini-pro:generateContent",
		"/v1beta/models/gemini-pro:streamGenerateContent",
		// Endpoints that carry no user text are still part of the surface: the
		// gate is about routing, not about whether there is anything to check.
		"/v1/files",
		"/v1/uploads",
		"/v1/batches",
		"/v1/fine_tuning/jobs",
		"/v1/vector_stores",
		"/v1/models",
		// Case is normalized, matching Extract.
		"/V1/Chat/Completions",
	}
	for _, p := range known {
		if !KnownPath(p) {
			t.Errorf("KnownPath(%q) = false, want true", p)
		}
	}

	unknown := []string{
		// Classic scanner probes: nothing here names an LLM endpoint.
		"/wp-login.php",
		"/.env",
		"/.git/config",
		"/phpmyadmin/index.php",
		"/actuator/env",
		"/druid/index.html",
		"/admin",
		"/xmlrpc.php",
		"/cgi-bin/luci",
		"/HNAP1",
		"/",
		// A bare /v1/ is not an endpoint; it is the prefix that covers them, and
		// prefix matching is the proxy's half of the allowlist, not this one.
		"/v1",
		"/v1/",
		// Handled by the proxy mux before the handler ever sees it.
		"/healthz",
	}
	for _, p := range unknown {
		if KnownPath(p) {
			t.Errorf("KnownPath(%q) = true, want false", p)
		}
	}
}

// TestKnownPathMatchersAreLoose pins the documented looseness rather than
// pretending it does not exist. The table's matchers are suffix/contains based
// because they exist to pick an extractor, not to validate a URL, so any path
// ending in a known shape reports known.
//
// That is acceptable only because a match means "not rejected by the path gate":
// the request is still extracted and scored, so the worst case is the pre-gate
// behavior. This test is the tripwire for that assumption — if the gate ever
// starts treating a match as "trusted, skip inspection", these cases become
// bypasses and this test must be replaced by a strict matcher.
func TestKnownPathMatchersAreLoose(t *testing.T) {
	for _, p := range []string{
		"/foo/models",          // contains a known suffix
		"/a/b/images/c",        // contains a known infix
		"/x/chat/completions",  // suffix matcher ignores the prefix
		"/v2/chat/completions", // a whole other version still matches
		"/random/messages",     // Anthropic matcher is a bare suffix
	} {
		if !KnownPath(p) {
			t.Errorf("KnownPath(%q) = false, want true (loose matcher)", p)
		}
	}
}

// TestKnownPathOnFormEndpoints documents why the multipart endpoints need no
// table entry: Extract dispatches them by content type before the path table is
// consulted (see the comment above endpoints), so the table deliberately omits
// them. The path gate therefore has to cover them some other way — and does,
// because the operator's default prefixes are /v1/ and /v1beta/, under which all
// of them sit. This is a property of their paths, and it would silently stop
// holding if a form endpoint were ever served outside those prefixes.
func TestKnownPathOnFormEndpoints(t *testing.T) {
	formEndpoints := []string{
		"/v1/audio/transcriptions",
		"/v1/audio/translations",
		"/v1/images/edits",
		"/v1/images/variations",
	}
	// The default prefix set, mirrored from config.DefaultPathAllowlistPrefixes.
	// Duplicated rather than imported because config imports extract, not the
	// other way around; the mirror is asserted in config's own tests.
	defaultPrefixes := []string{"/v1/", "/v1beta/"}
	for _, p := range formEndpoints {
		covered := false
		for _, prefix := range defaultPrefixes {
			if strings.HasPrefix(p, prefix) {
				covered = true
				break
			}
		}
		if !covered && !KnownPath(p) {
			t.Errorf("%q is neither table-known nor under a default prefix, so the path gate would reject it", p)
		}
	}
}
