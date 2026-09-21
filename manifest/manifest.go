// Package manifest parses and validates nook.json.
package manifest

import (
	"encoding/json"
	"fmt"
	"net"
	"regexp"
	"strings"
)

const FileName = "nook.json"

// A service nook may declare at most this many of each. The caps exist so a manifest stays
// something a person can read in one go, not because the runtime could not take more.
const (
	maxSecrets = 20
	maxEgress  = 20
	maxCron    = 5
	maxCommand = 500
)

var (
	nameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}[a-z0-9]$`)
	secretRe = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
	hostRe   = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?(\.[a-z0-9]([a-z0-9-]*[a-z0-9])?)+$`)
	reserved = map[string]bool{"api": true, "www": true, "app": true, "admin": true, "static": true, "mail": true, "dashboard": true, "nook": true}
	types    = map[string]bool{"static": true, "service": true}
	sources  = map[string]bool{"editors": true, "viewers": true}
)

// A CronEntry runs `Run` inside the machine on `Schedule`, five-field cron in UTC.
type CronEntry struct {
	Schedule string `json:"schedule"`
	Run      string `json:"run"`
}

type Manifest struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Entry string `json:"entry,omitempty"`

	// Service nooks only. See DESIGN.md §10b.
	// Source is who may read the run code: "editors" (owner and editors, the default) or
	// "viewers" (anyone who can open the nook, as a static nook's files always are).
	Source  string      `json:"source,omitempty"`
	Run     string      `json:"run,omitempty"`
	Port    int         `json:"port,omitempty"`
	Secrets []string    `json:"secrets,omitempty"`
	Egress  []string    `json:"egress,omitempty"`
	Cron    []CronEntry `json:"cron,omitempty"`
}

func (m *Manifest) IsService() bool { return m.Type == "service" }

func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("nook.json: %w", err)
	}
	if m.Type == "" {
		m.Type = "static"
	}
	if m.Type == "static" && m.Entry == "" {
		m.Entry = "index.html"
	}
	// Written out explicitly, so a pull or an export of a service nook states who can read it.
	if m.Type == "service" && m.Source == "" {
		m.Source = "editors"
	}
	if err := m.Validate(); err != nil {
		return nil, err
	}
	return &m, nil
}

func (m *Manifest) Validate() error {
	if !nameRe.MatchString(m.Name) {
		return fmt.Errorf("nook.json: name %q must be 2-40 chars of a-z, 0-9, and hyphens", m.Name)
	}
	if reserved[m.Name] {
		return fmt.Errorf("nook.json: name %q is reserved", m.Name)
	}
	if !types[m.Type] {
		return fmt.Errorf("nook.json: type %q is not a nook type (static or service)", m.Type)
	}
	if m.IsService() {
		return m.validateService()
	}
	if strings.HasPrefix(m.Entry, "/") || strings.Contains(m.Entry, "..") {
		return fmt.Errorf("nook.json: entry must be a relative path")
	}
	for _, f := range []struct {
		name string
		set  bool
	}{{"source", m.Source != ""}, {"run", m.Run != ""}, {"port", m.Port != 0}, {"secrets", len(m.Secrets) > 0}, {"egress", len(m.Egress) > 0}, {"cron", len(m.Cron) > 0}} {
		if f.set {
			return fmt.Errorf(`nook.json: %s only applies to a service nook; set "type": "service" or remove it`, f.name)
		}
	}
	return nil
}

func (m *Manifest) validateService() error {
	if m.Entry != "" {
		return fmt.Errorf(`nook.json: entry is for static nooks; a service nook serves its own routes from run`)
	}
	if m.Source != "" && !sources[m.Source] {
		return fmt.Errorf(`nook.json: source %q must be "editors" (the default) or "viewers"`, m.Source)
	}
	if err := command("run", m.Run); err != nil {
		return err
	}
	if m.Port < 1024 || m.Port > 65535 {
		return fmt.Errorf("nook.json: port %d must be between 1024 and 65535; it is the port run listens on", m.Port)
	}
	if len(m.Secrets) > maxSecrets {
		return fmt.Errorf("nook.json: at most %d secrets", maxSecrets)
	}
	seen := map[string]bool{}
	for _, s := range m.Secrets {
		switch {
		case !secretRe.MatchString(s):
			return fmt.Errorf("nook.json: secret name %q must be uppercase letters, digits, and underscores, like SLACK_TOKEN", s)
		case strings.HasPrefix(s, "NOOK_"):
			return fmt.Errorf("nook.json: secret name %q is reserved; NOOK_ names are set by the runtime", s)
		case seen[s]:
			return fmt.Errorf("nook.json: secret %q is listed twice", s)
		}
		seen[s] = true
		// Only the names live here. Values arrive by `nook secret set`.
	}
	if len(m.Egress) > maxEgress {
		return fmt.Errorf("nook.json: at most %d egress hosts", maxEgress)
	}
	for i, h := range m.Egress {
		host, err := egressHost(h)
		if err != nil {
			return err
		}
		m.Egress[i] = host
	}
	if len(m.Cron) > maxCron {
		return fmt.Errorf("nook.json: at most %d cron entries", maxCron)
	}
	for _, c := range m.Cron {
		if _, err := ParseSchedule(c.Schedule); err != nil {
			return fmt.Errorf("nook.json: %w", err)
		}
		if err := command("cron run", c.Run); err != nil {
			return err
		}
	}
	return nil
}

// egressHost accepts a bare hostname or IP. Anything with a scheme, path, port, or wildcard is
// rejected by name, because a nook author who wrote one meant something we would not enforce.
func egressHost(h string) (string, error) {
	host := strings.ToLower(strings.TrimSpace(h))
	bad := func(why string) error {
		return fmt.Errorf("nook.json: egress %q %s; list the host on its own, like \"hooks.slack.com\"", h, why)
	}
	switch {
	case host == "":
		return "", bad("is empty")
	case strings.Contains(host, "://"):
		return "", bad("has a scheme")
	case strings.ContainsAny(host, "/?# "):
		return "", bad("is a URL, not a host")
	case strings.Contains(host, "*"):
		return "", bad("is a wildcard, which egress rules cannot enforce")
	case strings.Contains(host, ":"):
		return "", bad("has a port; egress is per host, every port")
	case len(host) > 253:
		return "", bad("is too long")
	case net.ParseIP(host) != nil || hostRe.MatchString(host):
		return host, nil
	}
	return "", bad("is not a hostname")
}

func command(field, s string) error {
	switch {
	case strings.TrimSpace(s) == "":
		return fmt.Errorf("nook.json: %s is required for a service nook, like \"python app.py\"", field)
	case strings.ContainsAny(s, "\n\r"):
		return fmt.Errorf("nook.json: %s must be a single line", field)
	case len(s) > maxCommand:
		return fmt.Errorf("nook.json: %s is longer than %d characters", field, maxCommand)
	}
	return nil
}

// SourceVisibleToViewers says whether anyone who can open the nook may read its code. A static
// nook's files reach every viewer's browser anyway, so they always are; a service nook's do not,
// and stay with the owner and editors unless the manifest opts in.
func (m *Manifest) SourceVisibleToViewers() bool {
	return !m.IsService() || m.Source == "viewers"
}
