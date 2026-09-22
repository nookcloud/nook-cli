package helpers

import (
	"strings"
	"testing"
)

// The helpers are the surface a nook author's agent writes against, so the things the skill file
// promises about them are worth pinning.
func TestHelpersAreWhatTheSkillDescribes(t *testing.T) {
	for name, src := range map[string]string{PythonName: Python, NodeName: Node} {
		if len(src) < 500 {
			t.Fatalf("%s is empty or truncated (%d bytes)", name, len(src))
		}
		for _, needed := range []string{"NOOK_DATA_URL", "NOOK_TOKEN", "X-Nook-As", "viewer"} {
			if !strings.Contains(src, needed) {
				t.Errorf("%s does not mention %s", name, needed)
			}
		}
		// Acting for a viewer has to be by token. An address would let a nook name anyone.
		if strings.Contains(src, "X-Nook-As: \"+email") || strings.Contains(src, "'X-Nook-As': email") {
			t.Errorf("%s sends an email as X-Nook-As", name)
		}
	}
	for _, fn := range []string{"def list", "def get", "def create", "def update", "def delete"} {
		if !strings.Contains(Python, fn) {
			t.Errorf("nook_data.py is missing %s", fn)
		}
	}
	for _, fn := range []string{"list(", "get(", "create(", "update(", "delete("} {
		if !strings.Contains(Node, fn) {
			t.Errorf("nook-data.js is missing %s", fn)
		}
	}
}
