package manifest

import "testing"

func TestParse(t *testing.T) {
	m, err := Parse([]byte(`{"name":"my-tool"}`))
	if err != nil || m.Type != "static" || m.Entry != "index.html" {
		t.Fatalf("defaults: %+v %v", m, err)
	}
	for _, bad := range []string{`{"name":"API"}`, `{"name":"a"}`, `{"name":"x","entry":"../etc"}`, `{"name":"ok","type":"service"}`} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}
