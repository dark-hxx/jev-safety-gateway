package config

import (
	"slices"
	"strings"
	"testing"
)

// TestDefaultPathAllowlist mirrors the default prefix string against the list
// the extraction table's own test documents (TestKnownPathOnFormEndpoints). The
// two live in different packages with opposite import directions, so the mirror
// cannot be a shared constant — this test is what keeps them from drifting.
func TestDefaultPathAllowlist(t *testing.T) {
	got := Settings{}.PathPrefixList() // zero value: empty field, so the fallback runs
	want := []string{"/v1/", "/v1beta/"}
	if !slices.Equal(got, want) {
		t.Errorf("fallback prefixes = %v, want %v", got, want)
	}
	if got := DefaultSettings().PathAllowlistPrefixes; got != DefaultPathAllowlistPrefixes {
		t.Errorf("DefaultSettings prefix field = %q, want %q", got, DefaultPathAllowlistPrefixes)
	}
}

// TestPathAllowlistDefaultsToOff guards the upgrade path, which is the whole
// reason the field is opt-in: load() decodes the stored blob on top of
// DefaultSettings(), so a default of true would start rejecting traffic the
// moment a binary carrying the field is deployed, with nothing in the stored
// settings to explain it.
func TestPathAllowlistDefaultsToOff(t *testing.T) {
	def := DefaultSettings()
	if def.PathAllowlistEnabled {
		t.Error("PathAllowlistEnabled defaults to true; an upgrade would change behavior silently")
	}
	if def.AbuseCountUnknownPath {
		t.Error("AbuseCountUnknownPath defaults to true; it is only meaningful with the gate on")
	}
	// The prefixes are still seeded while the gate is off, so switching it on in
	// the console is immediately usable rather than an empty box.
	if def.PathAllowlistPrefixes == "" {
		t.Error("PathAllowlistPrefixes is seeded empty, so enabling the gate needs hand-editing")
	}
}

func TestPathPrefixList(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want []string
	}{
		{"the default passes through", "/v1/,/v1beta/", []string{"/v1/", "/v1beta/"}},
		{"whitespace is trimmed", "  /v1/ ,  /v1beta/ ", []string{"/v1/", "/v1beta/"}},
		{"empty entries are dropped", "/v1/,,/v1beta/,", []string{"/v1/", "/v1beta/"}},
		// The load-bearing case: without a leading slash the prefix could never
		// match (paths always start with "/"), so the gate would reject exactly
		// the traffic the operator meant to allow.
		{"a missing leading slash is added", "v1,v1beta", []string{"/v1", "/v1beta"}},
		// ...and nothing else is invented. A trailing slash is left exactly as
		// written: "/v1" also covers "/v1beta/...", which is more permissive (the
		// safe direction, and a prefix only means "not gated here" — content is
		// still filtered), while silently rewriting it to "/v1/" would be the
		// parser guessing at intent.
		{"a trailing slash is not invented", "/v1", []string{"/v1"}},
		{"a single prefix is fine", "/openai/", []string{"/openai/"}},
		{"a deep prefix is kept verbatim", "/api/v1/", []string{"/api/v1/"}},
		// An empty or unusable field falls back to the default rather than to an
		// empty list: an empty list would reject everything the table does not
		// recognize, i.e. a cleared text box would become a denial of service.
		{"empty falls back to the default", "", []string{"/v1/", "/v1beta/"}},
		{"only separators falls back to the default", " , , ", []string{"/v1/", "/v1beta/"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Settings{PathAllowlistPrefixes: c.in}.PathPrefixList()
			if !slices.Equal(got, c.want) {
				t.Errorf("PathPrefixList(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestUpdateSettingsCanonicalizesPrefixes verifies the store persists what the
// proxy will actually match on, so the console never shows a value that behaves
// differently from the one displayed.
func TestUpdateSettingsCanonicalizesPrefixes(t *testing.T) {
	s := openRuleStore(t)

	set := DefaultSettings()
	set.PathAllowlistEnabled = true
	set.PathAllowlistPrefixes = " v1 , , v1beta "
	if err := s.UpdateSettings(set); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if got := s.Settings().PathAllowlistPrefixes; got != "/v1,/v1beta" {
		t.Errorf("stored prefixes = %q, want %q", got, "/v1,/v1beta")
	}

	// An emptied field must come back as the default, not as "", because "" is
	// what the proxy would then treat as "no prefixes configured".
	set.PathAllowlistPrefixes = "   "
	if err := s.UpdateSettings(set); err != nil {
		t.Fatalf("UpdateSettings: %v", err)
	}
	if got := s.Settings().PathAllowlistPrefixes; got != DefaultPathAllowlistPrefixes {
		t.Errorf("stored prefixes after clearing = %q, want the default %q", got, DefaultPathAllowlistPrefixes)
	}

	// A prefix containing a comma cannot survive the round trip; the field is
	// comma-separated by definition, so this documents the boundary rather than
	// pretending the format handles it.
	set.PathAllowlistPrefixes = "/a,b/"
	_ = s.UpdateSettings(set)
	if got := s.Settings().PathAllowlistPrefixes; strings.Contains(got, "/a,b/") {
		t.Errorf("stored prefixes = %q, want the comma split into two entries", got)
	}
}
