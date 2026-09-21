package main

import (
	"bufio"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strings"
)

// A secret's value is never an argument: argv shows up in shell history and in ps. It comes from
// a pipe, or from a prompt with the terminal's echo off. It is never printed back, by any command.

func secret(args []string, asJSON bool) error {
	if len(args) == 0 {
		return fmt.Errorf("usage: nook secret set|list|unset [nook] [KEY]")
	}
	sub, rest := args[0], args[1:]
	name, rest, err := nookName(rest, isSecretName)
	if err != nil {
		return err
	}
	switch sub {
	case "list", "ls":
		out, err := call(http.MethodGet, "/v1/nooks/"+name+"/secrets", nil, "")
		if err != nil {
			return err
		}
		return emit(asJSON, out, secretLines(out))
	case "set":
		if len(rest) == 0 {
			return fmt.Errorf("usage: nook secret set [nook] KEY  (the value comes from stdin or a prompt)")
		}
		value, err := readSecretValue(rest[0])
		if err != nil {
			return err
		}
		out, err := callJSON(http.MethodPut, "/v1/nooks/"+name+"/secrets/"+rest[0], map[string]string{"value": value})
		if err != nil {
			return err
		}
		return emit(asJSON, out, fmt.Sprintf("%s is set for %s. its value is never shown again.\n%s", rest[0], name, secretLines(out)))
	case "unset", "rm":
		if len(rest) == 0 {
			return fmt.Errorf("usage: nook secret unset [nook] KEY")
		}
		out, err := call(http.MethodDelete, "/v1/nooks/"+name+"/secrets/"+rest[0], nil, "")
		if err != nil {
			return err
		}
		return emit(asJSON, out, fmt.Sprintf("%s is gone from %s\n%s", rest[0], name, secretLines(out)))
	}
	return fmt.Errorf("nook secret set|list|unset")
}

// isSecretName keeps an uppercase KEY from being mistaken for the nook's name.
func isSecretName(a string) bool {
	return a != "" && a == strings.ToUpper(a) && strings.Trim(a, "ABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") == ""
}

func readSecretValue(key string) (string, error) {
	st, _ := os.Stdin.Stat()
	piped := st != nil && st.Mode()&os.ModeCharDevice == 0
	if piped {
		b, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if v := strings.TrimRight(b, "\r\n"); v != "" {
			return v, nil
		}
		if err != nil {
			return "", fmt.Errorf("no value on stdin for %s", key)
		}
		return "", fmt.Errorf("%s cannot be empty", key)
	}
	fmt.Fprintf(os.Stderr, "value for %s (not shown): ", key)
	restore := echoOff()
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	restore()
	fmt.Fprintln(os.Stderr)
	if err != nil && strings.TrimSpace(line) == "" {
		return "", fmt.Errorf("no value given for %s", key)
	}
	v := strings.TrimRight(line, "\r\n")
	if v == "" {
		return "", fmt.Errorf("%s cannot be empty", key)
	}
	return v, nil
}

// echoOff hides typing through stty, so the CLI needs no dependency for one prompt. Where stty
// is not there the value echoes, which is worth saying rather than silently showing it.
func echoOff() func() {
	if stty("-echo") != nil {
		fmt.Fprint(os.Stderr, "\n(cannot hide input on this terminal; it will be visible) ")
		return func() {}
	}
	return func() { stty("echo") }
}

func stty(arg string) error {
	cmd := exec.Command("stty", arg)
	cmd.Stdin = os.Stdin // stty acts on the terminal it is given, which is the one being typed at
	return cmd.Run()
}

func secretLines(out map[string]any) string {
	set := map[string]any{}
	if list, ok := out["secrets"].([]any); ok {
		for _, s := range list {
			if m, ok := s.(map[string]any); ok {
				set[fmt.Sprintf("%v", m["name"])] = m["updated_by"]
			}
		}
	}
	var b strings.Builder
	seen := map[string]bool{}
	if d, ok := out["declared"].([]any); ok {
		for _, name := range d {
			k := fmt.Sprintf("%v", name)
			seen[k] = true
			if by, ok := set[k]; ok {
				fmt.Fprintf(&b, "  %s  set by %v\n", k, by)
			} else {
				fmt.Fprintf(&b, "  %s  not set\n", k)
			}
		}
	}
	for k, by := range set {
		if !seen[k] {
			fmt.Fprintf(&b, "  %s  set by %v, not declared in nook.json\n", k, by)
		}
	}
	if b.Len() == 0 {
		return "  no secrets"
	}
	if m, ok := out["missing"].([]any); ok && len(m) > 0 {
		fmt.Fprintf(&b, "\nthis nook cannot start until %d more %s set.", len(m), plural(len(m), "is", "are"))
	}
	return strings.TrimRight(b.String(), "\n")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return one
	}
	return many
}
