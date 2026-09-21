// Package manifest parses and validates nook.json.
package manifest

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

const FileName = "nook.json"

var (
	nameRe   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,38}[a-z0-9]$`)
	reserved = map[string]bool{"api": true, "www": true, "app": true, "admin": true, "static": true, "mail": true, "dashboard": true, "nook": true}
)

type Manifest struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Entry string `json:"entry,omitempty"`
}

func Parse(b []byte) (*Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("nook.json: %w", err)
	}
	if m.Type == "" {
		m.Type = "static"
	}
	if m.Entry == "" {
		m.Entry = "index.html"
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
	if m.Type != "static" {
		return fmt.Errorf("nook.json: type %q not supported yet (only static)", m.Type)
	}
	if strings.HasPrefix(m.Entry, "/") || strings.Contains(m.Entry, "..") {
		return fmt.Errorf("nook.json: entry must be a relative path")
	}
	return nil
}
