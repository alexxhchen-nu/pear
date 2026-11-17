package rosetta

import (
	"encoding/json"
	"testing"
)

// FuzzSanitizeAndRewriteRequest checks the rewrite layer never turns a valid
// request into invalid JSON, since that would be an opaque upstream 400.
func FuzzSanitizeAndRewriteRequest(f *testing.F) {
	f.Add([]byte(`{"model":"a","messages":[{"role":"user","content":"hi"}]}`), "b", "anthropic")
	f.Add([]byte(`{"model":"a","messages":[],"signature":"deadbeef"}`), "b", "openai")
	f.Add([]byte(`{"model":"a","stream":true}`), "", "responses")
	f.Add([]byte(`not json`), "b", "openai")

	f.Fuzz(func(t *testing.T, body []byte, model, proto string) {
		out := SanitizeAndRewriteRequest(body, model, proto)
		if json.Valid(body) && !json.Valid(out) {
			t.Fatalf("valid JSON in, invalid JSON out: %q", out)
		}
	})
}
