package router

import "testing"

// FuzzInspectRequest guards the request classifier that every proxied call
// runs through: malformed JSON must degrade, never panic.
func FuzzInspectRequest(f *testing.F) {
	f.Add([]byte(`{"model":"claude-3","stream":true}`), "claude-3", "")
	f.Add([]byte(`{"model":"gpt-4o","max_tokens":1024,"tools":[]}`), "", "")
	f.Add([]byte(`{`), "", "/tmp/project")
	f.Add([]byte(``), "", "")

	f.Fuzz(func(t *testing.T, body []byte, model, projectDir string) {
		feat := InspectRequest(body, model, projectDir)
		if again := InspectRequest(body, model, projectDir); again.SessionHash != feat.SessionHash {
			t.Fatalf("unstable session hash for identical input")
		}
	})
}
