package main

import (
	"os"
	"strings"
	"testing"
)

// piped runs readSecretValue with b on stdin, the way a shell pipe delivers it.
func piped(t *testing.T, b string) (string, error) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stdin
	os.Stdin = r
	t.Cleanup(func() { os.Stdin = old })
	go func() { w.WriteString(b); w.Close() }()
	return readSecretValue("KEY")
}

func TestPipedValueIsNotTruncated(t *testing.T) {
	pem := "-----BEGIN PRIVATE KEY-----\nMIIEvQIBADAN\nBgkqhkiG9w0B\n-----END PRIVATE KEY-----\n"
	got, err := piped(t, pem)
	if err != nil {
		t.Fatal(err)
	}
	// One trailing newline comes off; every other line stays. Truncating at the first newline
	// would store a value that looks set and fails at runtime.
	if want := strings.TrimSuffix(pem, "\n"); got != want {
		t.Errorf("got %q, want %q", got, want)
	}
	if strings.Count(got, "\n") != 3 {
		t.Errorf("lost lines: %q", got)
	}
}

func TestPipedValueKeepsWhatMatters(t *testing.T) {
	for name, c := range map[string]struct{ in, want string }{
		"one trailing newline comes off": {"abc\n", "abc"},
		"only one comes off":             {"abc\n\n", "abc\n"},
		"crlf comes off":                 {"abc\r\n", "abc"},
		"no newline is fine":             {"abc", "abc"},
		"inner spacing is kept":          {"a b\tc \n", "a b\tc "},
		"json survives":                  {"{\n  \"k\": \"v\"\n}\n", "{\n  \"k\": \"v\"\n}"},
	} {
		got, err := piped(t, c.in)
		if err != nil || got != c.want {
			t.Errorf("%s: got %q (%v), want %q", name, got, err, c.want)
		}
	}
	if _, err := piped(t, "\n"); err == nil {
		t.Error("a newline alone is an empty value")
	}
	if _, err := piped(t, ""); err == nil {
		t.Error("nothing piped is an empty value")
	}
}

func TestCheckSecretName(t *testing.T) {
	for _, ok := range []string{"A", "SLACK_TOKEN", "A1", "A_B_C"} {
		if err := checkSecretName(ok); err != nil {
			t.Errorf("%q should be a name: %v", ok, err)
		}
	}
	// These reached the server before and came back as a 400.
	for _, bad := range []string{"", "_FOO", "123", "lower", "WITH-DASH", "WITH SPACE", "NOOK_TOKEN", strings.Repeat("A", 65)} {
		if err := checkSecretName(bad); err == nil {
			t.Errorf("%q should be refused here, not by the server", bad)
		}
	}
	// Still recognised as an intended key, so the error names the key rather than the nook.
	for _, k := range []string{"_FOO", "123", "SLACK_TOKEN"} {
		if !isSecretName(k) {
			t.Errorf("%q should be read as a key", k)
		}
	}
	if isSecretName("board") {
		t.Error("a nook name is not a key")
	}
}
