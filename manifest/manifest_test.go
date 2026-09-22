package manifest

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestParseStatic(t *testing.T) {
	m, err := Parse([]byte(`{"name":"my-tool"}`))
	if err != nil || m.Entry != "index.html" {
		t.Fatalf("defaults: %+v %v", m, err)
	}
	if m.IsService() {
		t.Error("a nook with no run command does not run one")
	}
	for _, bad := range []string{
		`{"name":"API"}`,
		`{"name":"a"}`,
		`{"name":"x","entry":"../etc"}`,
		// A field that only means something alongside "run" is a mistake worth naming.
		`{"name":"ok","port":8000}`,
		`{"name":"ok","secrets":["TOKEN"]}`,
		`{"name":"ok","egress":["example.com"]}`,
		`{"name":"ok","cron":[{"schedule":"* * * * *","run":"x"}]}`,
		`{"name":"ok","source":"viewers"}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("expected error for %s", bad)
		}
	}
}

func TestParseService(t *testing.T) {
	m, err := Parse([]byte(`{ "name": "board", "run": "python app.py", "port": 8000,
		"secrets": ["SLACK_TOKEN"], "egress": ["hooks.slack.com", "API.example.com"],
		"cron": [{ "schedule": "0 9 * * 1-5", "run": "python digest.py" }] }`))
	if err != nil {
		t.Fatalf("the manifest from DESIGN.md §10b must parse: %v", err)
	}
	if !m.IsService() || m.Run != "python app.py" || m.Port != 8000 {
		t.Fatalf("service fields: %+v", m)
	}
	if m.Entry != "" {
		t.Errorf("a service nook has no entry, got %q", m.Entry)
	}
	// Unset means the run code stays with the owner and editors, and says so when written back.
	if m.Source != "editors" || m.SourceVisibleToViewers() {
		t.Errorf("source defaults closed, got %q", m.Source)
	}
	if m.Egress[1] != "api.example.com" {
		t.Errorf("egress hosts are lowercased, got %q", m.Egress[1])
	}
	if len(m.Secrets) != 1 || len(m.Cron) != 1 {
		t.Errorf("secrets and cron: %+v", m)
	}
}

func TestServiceValidation(t *testing.T) {
	for name, bad := range map[string]string{
		"run missing":      `{"name":"board","port":8000}`,
		"run blank":        `{"name":"board","run":"  ","port":8000}`,
		"run multiline":    `{"name":"board","run":"a\nb","port":8000}`,
		"port missing":     `{"name":"board","run":"x"}`,
		"port privileged":  `{"name":"board","run":"x","port":80}`,
		"port too high":    `{"name":"board","run":"x","port":70000}`,
		"entry set":        `{"name":"board","run":"x","port":8000,"entry":"index.html"}`,
		"secret lowercase": `{"name":"board","run":"x","port":8000,"secrets":["token"]}`,
		"secret reserved":  `{"name":"board","run":"x","port":8000,"secrets":["NOOK_TOKEN"]}`,
		"secret twice":     `{"name":"board","run":"x","port":8000,"secrets":["A","A"]}`,
		"egress url":       `{"name":"board","run":"x","port":8000,"egress":["https://a.com"]}`,
		"egress path":      `{"name":"board","run":"x","port":8000,"egress":["a.com/hooks"]}`,
		"egress port":      `{"name":"board","run":"x","port":8000,"egress":["a.com:443"]}`,
		"egress wildcard":  `{"name":"board","run":"x","port":8000,"egress":["*.slack.com"]}`,
		"egress bare word": `{"name":"board","run":"x","port":8000,"egress":["localhost"]}`,
		"cron schedule":    `{"name":"board","run":"x","port":8000,"cron":[{"schedule":"0 9 * *","run":"y"}]}`,
		"cron run blank":   `{"name":"board","run":"x","port":8000,"cron":[{"schedule":"* * * * *","run":""}]}`,
		"source unknown":   `{"name":"board","run":"x","port":8000,"source":"everyone"}`,
		"source public":    `{"name":"board","run":"x","port":8000,"source":"public"}`,
	} {
		if _, err := Parse([]byte(bad)); err == nil {
			t.Errorf("%s: expected an error for %s", name, bad)
		}
	}
	// An IP is easier to enforce than a hostname, so it is allowed.
	if _, err := Parse([]byte(`{"name":"board","run":"x","port":8000,"egress":["10.0.0.1"]}`)); err != nil {
		t.Errorf("egress may be an IP: %v", err)
	}
	// No egress at all is valid and means no outbound network.
	if m, err := Parse([]byte(`{"name":"board","run":"x","port":8000}`)); err != nil || len(m.Egress) != 0 {
		t.Errorf("egress is optional: %v", err)
	}
}

func TestSourceVisibility(t *testing.T) {
	// A static nook's files reach the browser anyway, so they are always readable.
	m, err := Parse([]byte(`{"name":"page"}`))
	if err != nil || !m.SourceVisibleToViewers() {
		t.Errorf("static source is always visible: %+v %v", m, err)
	}
	if m.Source != "" {
		t.Errorf("static nooks carry no source field, got %q", m.Source)
	}
	open, err := Parse([]byte(`{"name":"board","run":"x","port":8000,"source":"viewers"}`))
	if err != nil || !open.SourceVisibleToViewers() {
		t.Errorf(`"viewers" opts in: %+v %v`, open, err)
	}
}

// Files written before the manifest stopped declaring a type still parse, and what they are
// follows from whether they have a run command rather than from what they claim to be.
func TestLegacyTypeIsAcceptedAndIgnored(t *testing.T) {
	page, err := Parse([]byte(`{"name":"old-page","type":"static"}`))
	if err != nil || page.IsService() || page.Entry != "index.html" {
		t.Fatalf("an old static manifest: %+v %v", page, err)
	}
	svc, err := Parse([]byte(`{"name":"old-svc","type":"service","run":"python app.py","port":8000}`))
	if err != nil || !svc.IsService() {
		t.Fatalf("an old service manifest: %+v %v", svc, err)
	}
	// A type that never existed is no longer an error, because the field no longer decides.
	odd, err := Parse([]byte(`{"name":"odd","type":"whatever"}`))
	if err != nil || odd.IsService() {
		t.Fatalf("an unknown type is ignored, not fatal: %+v %v", odd, err)
	}
	// And it is never written back out, so a pull or an export does not teach it to anyone.
	b, err := json.Marshal(svc)
	if err != nil || strings.Contains(string(b), `"type"`) {
		t.Errorf("type must not be written back: %s", b)
	}
	// A nook that claims to be a service without a run command is simply a page.
	lying, err := Parse([]byte(`{"name":"lying","type":"service"}`))
	if err != nil || lying.IsService() {
		t.Fatalf("claiming to be a service is not being one: %+v %v", lying, err)
	}
}
