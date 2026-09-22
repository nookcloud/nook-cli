// nook is the Nook CLI. Agents are its primary user: non-interactive, --json everywhere.
package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"github.com/nookcloud/nook-cli/manifest"
)

var version = "dev"

const maxDeployBytes = 10 << 20

func main() {
	if len(os.Args) < 2 {
		usage()
		os.Exit(2)
	}
	args, asJSON := stripJSON(os.Args[2:])
	var err error
	cmd := os.Args[1]
	// `nook help <command>` and `nook <command> --help` say what one command does.
	if cmd == "help" || cmd == "--help" || cmd == "-h" {
		if len(args) == 0 {
			usage()
			os.Exit(0)
		}
		os.Exit(help(args[0]))
	}
	for _, a := range args {
		if a == "--help" || a == "-h" {
			os.Exit(help(cmd))
		}
	}
	// `nook account <sub>` groups the account commands; the bare names still work.
	if cmd == "account" {
		if len(args) == 0 {
			usage()
			os.Exit(2)
		}
		cmd, args = args[0], args[1:]
	}
	switch cmd {
	case "login":
		err = login(args, asJSON)
	case "init":
		err = initNook(args, asJSON)
	case "deploy":
		err = deploy(args, asJSON)
	case "share":
		err = share(args, asJSON)
	case "open":
		err = openNook(args, asJSON)
	case "list", "ls":
		err = list(asJSON)
	case "pull":
		err = pull(args, asJSON)
	case "remix":
		err = remix(args, asJSON)
	case "unshare":
		err = unshare(args, asJSON)
	case "mode":
		err = mode(args, asJSON)
	case "transfer":
		err = transfer(args, asJSON)
	case "versions":
		err = versions(args, asJSON)
	case "rollback":
		err = rollback(args, asJSON)
	case "export":
		err = exportNook(args, asJSON)
	case "import":
		err = importNook(args, asJSON)
	case "data":
		err = data(args, asJSON)
	case "secret", "secrets":
		err = secret(args, asJSON)
	case "delete":
		err = deleteNook(args, asJSON)
	case "mcp":
		err = mcpServe()
	case "whoami":
		err = whoami(asJSON)
	case "plan":
		err = plan(asJSON)
	case "upgrade":
		err = upgrade(asJSON)
	case "logout":
		err = os.Remove(tokenPath())
	case "stats":
		err = stats(asJSON)
	case "version":
		fmt.Println("nook", version)
	default:
		usage()
		os.Exit(2)
	}
	if err != nil {
		if asJSON {
			json.NewEncoder(os.Stdout).Encode(map[string]string{"error": err.Error()})
		} else {
			fmt.Fprintln(os.Stderr, "nook:", err)
		}
		os.Exit(1)
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, `nook: deploy a nook, a small tool at its own URL, with one command. share it like a doc.

  login                        sign in (opens a browser)
  init [name]                  create nook.json and a starter page here
  deploy [dir]                 deploy this folder as a nook
  share [nook] <email> [--role viewer|editor]
  open [nook]                  open the nook in a browser
  list                         your nooks, and nooks shared with you
  pull <nook> [dir]            download a nook's files to edit and redeploy
  remix <nook> [--as name]     copy a nook you can open into one you own, without its data

more:
  unshare [nook] <email>       mode [nook] <private|org|link|public>
  transfer [nook] <email>      versions [nook]      rollback [nook] [version]
  export [nook] [-o file]      import <file> [--name n] [--share]
  data <nook> list|get|create|update|delete|export|import <collection> ...
  secret set|list|unset [nook] KEY      values come from stdin or a prompt, never argv
  delete [nook]                mcp (run the MCP server on stdio)
  account whoami|plan|upgrade|logout|stats
  help <command>               what one command does; --help after any command works too

[nook] defaults to the name in ./nook.json. every command takes --json.
env: NOOK_API (default https://getnook.dev), NOOK_TOKEN`)
}

// helpText is what each command does, in the words a person needs to use it. Generated docs read
// these verbatim, so a line here is a line on the docs site.
var helpText = map[string]string{
	"login": `nook login [--dev <email>]
  sign in. opens the browser to getnook.dev and stores a token for this server under your
  config directory. --dev <email> signs in as that email against a local server started
  with NOOK_DEV_LOGIN=1.`,
	"init": `nook init [name]
  write a nook.json and a starter index.html in this folder. the name is the subdomain;
  it defaults to the folder's name.`,
	"deploy": `nook deploy [dir]
  deploy the folder as a nook. reads nook.json for the name and uploads every file not
  ignored by .nookignore; .git, node_modules, editor and agent folders, and .env are always
  skipped. prints the url and the new version number. a nook with a run command in its
  manifest runs it on a server.`,
	"share": `nook share [nook] <email> [--role viewer|editor]
  give one person access. viewer opens and uses the nook; editor also edits its data and
  reads its code. the person signs in with that email.`,
	"unshare": `nook unshare [nook] <email>
  take one person's access away.`,
	"mode": `nook mode [nook] <private|org|link|public>
  who can open the nook besides the people it is shared with. private: nobody. org: anyone
  who signs in with an email at your domain. link: anyone with the address, signed in.
  public: anyone, signed in or not, read-only.`,
	"open": `nook open [nook]
  open the nook in a browser.`,
	"list": `nook list
  your nooks, then the nooks other people have shared with you.`,
	"pull": `nook pull <nook> [dir]
  download a nook's files to a folder, to edit and redeploy. files only; data stays where
  it is. you need to be able to read the nook's code.`,
	"remix": `nook remix <nook> [--as name]
  copy a nook you can open into a new one you own, without its data. the new nook is
  private until you share it.`,
	"transfer": `nook transfer [nook] <email>
  make someone else the owner. you stay on as an editor. any secrets are cleared and the
  new owner is told which names to set.`,
	"versions": `nook versions [nook]
  every deploy of the nook, newest first, with its version number.`,
	"rollback": `nook rollback [nook] [version]
  serve an earlier version again. defaults to the one before the current. rolls back the
  files and the manifest together; data is untouched.`,
	"export": `nook export [nook] [-o file]
  download the whole nook, files and database, as one archive.`,
	"import": `nook import <file> [--name n] [--share]
  create a nook from an archive made by nook export. --share keeps the sharing that was
  recorded in the archive.`,
	"data": `nook data <nook> list|get|create|update|delete|export|import <collection> ...
  work with the nook's database from the terminal.
    list <collection> [--limit n] [--order asc|desc]
    get <collection> <id>
    create <collection> '<json>'
    update <collection> <id> '<json>'
    delete <collection> <id>
    export <collection> [--csv]          json unless --csv; writes to stdout
    import <collection> <file.csv>
  collections starting with me/ are per-person.`,
	"secret": `nook secret set|list|unset [nook] KEY
  secrets for a nook that runs code. set reads the value from stdin or a hidden prompt,
  never from the command line, and it is never shown again. list shows which declared
  names are set and by whom. unset removes one. only the owner may do this.`,
	"delete": `nook delete [nook]
  delete the nook, its files, and its database. there is no undo; nook export first.`,
	"mcp": `nook mcp
  run the mcp server on stdin and stdout, for an agent to call. every command here is a
  tool, except secrets, on purpose.`,
	"account": `nook account whoami|plan|upgrade|logout|stats
  whoami: who you are signed in as. plan: your plan and what you are using of it.
  upgrade: open billing. logout: forget the token for this server. stats: platform
  totals, admins only. the bare names work too, without "account".`,
	"version": `nook version
  print the version of this binary.`,
}

// help prints one command's text and returns the exit code.
func help(cmd string) int {
	switch cmd {
	case "ls":
		cmd = "list"
	case "secrets":
		cmd = "secret"
	case "whoami", "plan", "upgrade", "logout", "stats":
		cmd = "account"
	}
	t, ok := helpText[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "nook: no command %q\n\n", cmd)
		usage()
		return 2
	}
	fmt.Fprintln(os.Stderr, t)
	return 0
}

func stripJSON(args []string) ([]string, bool) {
	var out []string
	asJSON := false
	for _, a := range args {
		if a == "--json" {
			asJSON = true
		} else {
			out = append(out, a)
		}
	}
	return out, asJSON
}

func flagValue(args []string, name string) (string, []string) {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1], append(append([]string{}, args[:i]...), args[i+2:]...)
		}
	}
	return "", args
}

func apiBase() string {
	if v := os.Getenv("NOOK_API"); v != "" {
		return strings.TrimRight(v, "/")
	}
	return "https://getnook.dev"
}

// tokenPath is per server, so a dev login against localhost never overwrites the production token.
func tokenPath() string {
	dir, err := os.UserConfigDir()
	if err != nil {
		dir = os.Getenv("HOME")
	}
	_, host, _ := strings.Cut(apiBase(), "://")
	host = strings.NewReplacer(":", "_", "/", "_").Replace(host)
	return filepath.Join(dir, "nook", "token-"+host)
}

func token() string {
	if v := os.Getenv("NOOK_TOKEN"); v != "" {
		return v
	}
	b, err := os.ReadFile(tokenPath())
	if err != nil && strings.HasSuffix(tokenPath(), "token-getnook.dev") {
		// Legacy single token file from before per-server tokens.
		b, _ = os.ReadFile(filepath.Join(filepath.Dir(tokenPath()), "token"))
	}
	return strings.TrimSpace(string(b))
}

func saveToken(t string) error {
	if err := os.MkdirAll(filepath.Dir(tokenPath()), 0o700); err != nil {
		return err
	}
	return os.WriteFile(tokenPath(), []byte(t+"\n"), 0o600)
}

// call performs an API request and decodes the JSON response, surfacing {"error": ...} as an error.
func call(method, path string, body io.Reader, contentType string) (map[string]any, error) {
	req, err := http.NewRequest(method, apiBase()+path, body)
	if err != nil {
		return nil, err
	}
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	if t := token(); t != "" {
		req.Header.Set("Authorization", "Bearer "+t)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("cannot reach %s: %w", apiBase(), err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("bad response (%s)", resp.Status)
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("%v", out["error"])
	}
	return out, nil
}

func callJSON(method, path string, v any) (map[string]any, error) {
	b, _ := json.Marshal(v)
	return call(method, path, bytes.NewReader(b), "application/json")
}

func emit(asJSON bool, v map[string]any, human string) error {
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(v)
	}
	fmt.Println(human)
	return nil
}

func login(args []string, asJSON bool) error {
	if dev, rest := flagValue(args, "--dev"); dev != "" {
		_ = rest
		out, err := callJSON(http.MethodPost, "/v1/dev/token", map[string]string{"email": dev})
		if err != nil {
			return err
		}
		if err := saveToken(out["token"].(string)); err != nil {
			return err
		}
		return emit(asJSON, map[string]any{"email": out["email"]}, fmt.Sprintf("signed in as %v (dev)", out["email"]))
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return err
	}
	port := ln.Addr().(*net.TCPAddr).Port
	got := make(chan string, 1)
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t := r.URL.Query().Get("token")
		if t == "" {
			http.Error(w, "missing token", http.StatusBadRequest)
			return
		}
		fmt.Fprintln(w, "signed in. you can close this tab.")
		got <- t
	})}
	go srv.Serve(ln)
	defer srv.Close()
	u := fmt.Sprintf("%s/cli/login?port=%d", apiBase(), port)
	fmt.Fprintf(os.Stderr, "opening %s\n", u)
	openBrowser(u)
	select {
	case t := <-got:
		if err := saveToken(t); err != nil {
			return err
		}
	case <-time.After(5 * time.Minute):
		return fmt.Errorf("timed out waiting for sign-in")
	}
	return whoami(asJSON)
}

func whoami(asJSON bool) error {
	out, err := call(http.MethodGet, "/v1/me", nil, "")
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("%v", out["email"]))
}

// nookName resolves which nook a command targets: --nook <name>, a positional that isn't the
// command's own argument (email, mode, version), or the name in ./nook.json.
func nookName(args []string, isOwnArg func(string) bool) (string, []string, error) {
	if name, rest := flagValue(args, "--nook"); name != "" {
		return name, rest, nil
	}
	var own, rest []string
	name := ""
	for _, a := range args {
		switch {
		case isOwnArg != nil && isOwnArg(a):
			own = append(own, a)
		case name == "" && !strings.HasPrefix(a, "-"):
			name = a
		default:
			rest = append(rest, a)
		}
	}
	if name != "" {
		return name, append(own, rest...), nil
	}
	mb, err := os.ReadFile(manifest.FileName)
	if err != nil {
		return "", args, fmt.Errorf("which nook? pass its name (nook %s <name> ...) or run inside a folder with %s", "<command>", manifest.FileName)
	}
	m, err := manifest.Parse(mb)
	if err != nil {
		return "", args, err
	}
	return m.Name, args, nil
}

func isEmail(a string) bool { return strings.Contains(a, "@") }
func isMode(a string) bool  { return a == "private" || a == "org" || a == "link" || a == "public" }
func isNumber(a string) bool {
	_, err := fmt.Sscanf(a, "%d", new(int))
	return err == nil && strings.Trim(a, "0123456789") == ""
}

// initNook scaffolds a nook: manifest, a starter page that already uses nook.js, and the agent skill.
func initNook(args []string, asJSON bool) error {
	name := ""
	if len(args) > 0 {
		name = args[0]
	}
	if name == "" {
		wd, _ := os.Getwd()
		name = strings.ToLower(filepath.Base(wd))
	}
	name = regexp.MustCompile(`[^a-z0-9-]+`).ReplaceAllString(strings.ToLower(name), "-")
	name = strings.Trim(name, "-")
	if len(name) < 2 {
		name = "my-nook"
	}
	if len(name) > 40 {
		name = name[:40]
	}
	if _, err := os.Stat(manifest.FileName); err == nil {
		return fmt.Errorf("%s already exists here", manifest.FileName)
	}
	m := manifest.Manifest{Name: name}
	if err := m.Validate(); err != nil {
		return err
	}
	// Just the name. Adding a "run" command is what makes a nook run code on a server.
	if err := os.WriteFile(manifest.FileName, []byte(fmt.Sprintf("{ \"name\": %q }\n", name)), 0o644); err != nil {
		return err
	}
	wrote := []string{manifest.FileName}
	if _, err := os.Stat("index.html"); err != nil {
		if err := os.WriteFile("index.html", []byte(strings.ReplaceAll(starterPage, "{{name}}", name)), 0o644); err != nil {
			return err
		}
		wrote = append(wrote, "index.html")
	}
	// Drop the agent skill where Claude Code and friends look for it, so "put this on nook" just works.
	if resp, err := http.Get(apiBase() + "/skill.md"); err == nil && resp.StatusCode == 200 {
		defer resp.Body.Close()
		if b, err := io.ReadAll(resp.Body); err == nil && os.MkdirAll(".claude/skills/nook", 0o755) == nil {
			if os.WriteFile(".claude/skills/nook/SKILL.md", b, 0o644) == nil {
				wrote = append(wrote, ".claude/skills/nook/SKILL.md")
			}
		}
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(map[string]any{"name": name, "files": wrote})
	}
	fmt.Printf("created %s\n", strings.Join(wrote, ", "))
	fmt.Printf("next: edit index.html, then `nook deploy` → https://%s.%s\n", name, hostOf(apiBase()))
	return nil
}

func hostOf(base string) string {
	_, host, _ := strings.Cut(base, "://")
	return host
}

const starterPage = `<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>{{name}}</title>
<style>
  body { font: 16px system-ui, sans-serif; margin: 2rem auto; max-width: 30rem; padding: 0 1rem; color: #1a1a1a }
  @media (prefers-color-scheme: dark) { body { background: #111; color: #eee } input, button { background: #222; color: #eee; border-color: #444 } }
  form { display: flex; gap: .5rem } input { flex: 1; padding: .5rem; font: inherit; border: 1px solid #ccc; border-radius: 6px } button { font: inherit; padding: .5rem .8rem; border: 1px solid #ccc; border-radius: 6px }
  li { padding: .4rem 0; border-bottom: 1px solid #ddd; list-style: none } ul { padding: 0 } small { color: #888 }
</style>
<h1>{{name}}</h1>
<p id="who"><small>loading…</small></p>
<form id="f" hidden><input id="text" placeholder="add something" required><button>add</button></form>
<ul id="list"></ul>
<script src="/_nook/nook.js"></script>
<script>
  // This starter stores items in a shared collection. See .claude/skills/nook/SKILL.md for the whole API.
  const $ = id => document.getElementById(id);
  const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
  async function render() {
    const { documents } = await nook.data.list("items", { order: "desc" });
    $("list").innerHTML = documents.map(d => "<li>" + esc(d.data.text) + " <small>" + esc(d.created_by) + "</small></li>").join("") || "<li><small>nothing yet</small></li>";
  }
  (async () => {
    const u = await nook.ready;
    $("who").innerHTML = "<small>" + (u.signedIn ? esc(u.name || u.email) + " · " + u.role : "guest") + "</small>";
    $("f").hidden = !u.canEdit;
    $("f").onsubmit = async e => { e.preventDefault(); await nook.data.create("items", { text: $("text").value }); $("f").reset(); render(); };
    render();
  })();
</script>
`

// deployDir tars and uploads a directory; shared by the CLI command and the MCP tool.
func deployDir(dir string) (map[string]any, error) {
	mb, err := os.ReadFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return nil, fmt.Errorf("no %s in %s (run `nook init` first)", manifest.FileName, dir)
	}
	if _, err := manifest.Parse(mb); err != nil {
		return nil, err
	}
	body, _, err := tarball(dir)
	if err != nil {
		return nil, err
	}
	return call(http.MethodPost, "/v1/deploy", body, "application/gzip")
}

func deploy(args []string, asJSON bool) error {
	dir := "."
	if len(args) > 0 {
		dir = args[0]
	}
	mb, err := os.ReadFile(filepath.Join(dir, manifest.FileName))
	if err != nil {
		return fmt.Errorf("no %s in %s (run `nook init` first)", manifest.FileName, dir)
	}
	m, err := manifest.Parse(mb)
	if err != nil {
		return err
	}
	body, n, err := tarball(dir)
	if err != nil {
		return err
	}
	if !asJSON {
		fmt.Fprintf(os.Stderr, "deploying %s (%d files, %s)...\n", m.Name, n, human(body.Len()))
	}
	out, err := call(http.MethodPost, "/v1/deploy", body, "application/gzip")
	if err != nil {
		return err
	}
	if !asJSON {
		if ws, _ := out["warnings"].([]any); len(ws) > 0 {
			for _, w := range ws {
				fmt.Fprintf(os.Stderr, "warning: %v\n", w)
			}
		}
	}
	return emit(asJSON, out, fmt.Sprintf("%s  v%v  (%v)", out["url"], out["version"], out["mode"]))
}

// nookURL derives a nook's origin from the API base: https://getnook.dev -> https://<name>.getnook.dev
func nookURL(name string) string {
	base := apiBase()
	scheme, host, _ := strings.Cut(base, "://")
	return scheme + "://" + name + "." + host
}

func data(args []string, asJSON bool) error {
	if len(args) < 3 {
		return fmt.Errorf("usage: nook data <nook> <list|get|create|update|delete|export> <collection> ...")
	}
	name, op, coll := args[0], args[1], args[2]
	rest := args[3:]
	base := nookURL(name) + "/_nook/data/" + coll
	do := func(method, u string, body io.Reader) (map[string]any, error) {
		req, err := http.NewRequest(method, u, body)
		if err != nil {
			return nil, err
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header.Set("Authorization", "Bearer "+token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return nil, err
		}
		defer resp.Body.Close()
		var out map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("bad response (%s)", resp.Status)
		}
		if resp.StatusCode >= 400 {
			return nil, fmt.Errorf("%v", out["error"])
		}
		return out, nil
	}
	switch op {
	case "list":
		order, rest := flagValue(rest, "--order")
		limit, _ := flagValue(rest, "--limit")
		q := "?"
		if order != "" {
			q += "order=" + order + "&"
		}
		if limit != "" {
			q += "limit=" + limit
		}
		out, err := do(http.MethodGet, base+q, nil)
		if err != nil {
			return err
		}
		if asJSON {
			return json.NewEncoder(os.Stdout).Encode(out)
		}
		for _, x := range out["documents"].([]any) {
			d := x.(map[string]any)
			b, _ := json.Marshal(d["data"])
			fmt.Printf("%s  %s  %s\n", d["id"], d["created_by"], b)
		}
		return nil
	case "get":
		if len(rest) < 1 {
			return fmt.Errorf("usage: nook data <nook> get <collection> <id>")
		}
		out, err := do(http.MethodGet, base+"/"+rest[0], nil)
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	case "create":
		if len(rest) < 1 {
			return fmt.Errorf("usage: nook data <nook> create <collection> <json|->")
		}
		var lines []string
		if rest[0] == "-" {
			b, _ := io.ReadAll(os.Stdin)
			for _, l := range strings.Split(string(b), "\n") {
				if strings.TrimSpace(l) != "" {
					lines = append(lines, l)
				}
			}
		} else {
			lines = []string{rest[0]}
		}
		n := 0
		for _, l := range lines {
			out, err := do(http.MethodPost, base, strings.NewReader(l))
			if err != nil {
				return fmt.Errorf("record %d: %w", n+1, err)
			}
			n++
			if asJSON {
				json.NewEncoder(os.Stdout).Encode(out)
			}
		}
		if !asJSON {
			fmt.Printf("created %d in %s/%s\n", n, name, coll)
		}
		return nil
	case "update":
		if len(rest) < 2 {
			return fmt.Errorf("usage: nook data <nook> update <collection> <id> <json>")
		}
		out, err := do(http.MethodPatch, base+"/"+rest[0], strings.NewReader(rest[1]))
		if err != nil {
			return err
		}
		return json.NewEncoder(os.Stdout).Encode(out)
	case "delete":
		if len(rest) < 1 {
			return fmt.Errorf("usage: nook data <nook> delete <collection> <id>")
		}
		out, err := do(http.MethodDelete, base+"/"+rest[0], nil)
		if err != nil {
			return err
		}
		return emit(asJSON, out, fmt.Sprintf("deleted %v", out["deleted"]))
	case "import":
		if len(rest) < 1 {
			return fmt.Errorf("usage: nook data <nook> import <collection> <file.csv>")
		}
		f, err := os.Open(rest[0])
		if err != nil {
			return err
		}
		defer f.Close()
		req, _ := http.NewRequest(http.MethodPost, nookURL(name)+"/_nook/import/"+coll, f)
		req.Header.Set("Content-Type", "text/csv")
		req.Header.Set("Authorization", "Bearer "+token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		var out map[string]any
		if json.NewDecoder(resp.Body).Decode(&out) != nil {
			return fmt.Errorf("import failed (%s)", resp.Status)
		}
		if resp.StatusCode >= 400 {
			return fmt.Errorf("%v", out["error"])
		}
		return emit(asJSON, out, fmt.Sprintf("imported %v rows into %s/%s", out["created"], name, coll))
	case "export":
		format := "json"
		if len(rest) > 0 && rest[0] == "--csv" {
			format = "csv"
		}
		req, _ := http.NewRequest(http.MethodGet, nookURL(name)+"/_nook/export/"+coll+"?format="+format, nil)
		req.Header.Set("Authorization", "Bearer "+token())
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return fmt.Errorf("export failed (%s)", resp.Status)
		}
		_, err = io.Copy(os.Stdout, resp.Body)
		return err
	}
	return fmt.Errorf("unknown data operation %q", op)
}

func stats(asJSON bool) error {
	out, err := call(http.MethodGet, "/v1/stats", nil, "")
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	t := out["totals"].(map[string]any)
	fmt.Printf("nooks %v · users %v · shares %v\n", t["nooks"], t["users"], t["grants"])
	for _, x := range out["days"].([]any) {
		d := x.(map[string]any)
		fmt.Printf("%s", d["day"])
		for k, v := range d["counts"].(map[string]any) {
			fmt.Printf("  %s=%v", k, v)
		}
		fmt.Println()
	}
	return nil
}

// limit renders a plan ceiling, where zero means unlimited.
func limit(v any) string {
	n, _ := v.(float64)
	if n == 0 {
		return "unlimited"
	}
	return fmt.Sprintf("%.0f", n)
}

func plan(asJSON bool) error {
	out, err := call(http.MethodGet, "/v1/billing/plan", nil, "")
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	u, _ := out["usage"].(map[string]any)
	l, _ := out["limits"].(map[string]any)
	mb := func(v any) string {
		n, _ := v.(float64)
		if n < 1<<20 {
			return fmt.Sprintf("%.0f KB", n/(1<<10))
		}
		return fmt.Sprintf("%.0f MB", n/(1<<20))
	}
	fmt.Printf("plan          %v\n", out["plan"])
	fmt.Printf("nooks         %.0f of %s\n", u["nooks"], limit(l["nooks"]))
	fmt.Printf("shared with   %.0f of %s  (most on any one nook)\n", u["collaborators"], limit(l["collaborators"]))
	if n, _ := l["bytes"].(float64); n == 0 {
		fmt.Printf("storage       %s of unlimited\n", mb(u["bytes"]))
	} else {
		fmt.Printf("storage       %s of %s\n", mb(u["bytes"]), mb(l["bytes"]))
	}
	if out["plan"] == "free" && out["billable"] == true {
		fmt.Printf("\nPro is %v for unlimited nooks and collaborators. run `nook upgrade`.\n", out["price"])
	}
	return nil
}

// upgrade opens Stripe checkout, or the billing portal for someone already on Pro.
func upgrade(asJSON bool) error {
	path := "/v1/billing/checkout"
	if p, err := call(http.MethodGet, "/v1/billing/plan", nil, ""); err == nil && p["plan"] == "pro" {
		path = "/v1/billing/portal"
	}
	out, err := call(http.MethodPost, path, nil, "application/json")
	if err != nil {
		return err
	}
	u, _ := out["url"].(string)
	if !asJSON {
		openBrowser(u)
	}
	return emit(asJSON, out, "open this to continue:\n"+u)
}

// pull downloads a nook's current files into a folder, ready to edit and `nook deploy` again.
func pull(args []string, asJSON bool) error {
	force := false
	var rest []string
	for _, a := range args {
		if a == "--force" {
			force = true
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: nook pull <nook> [dir] [--force]")
	}
	name := rest[0]
	dir := name
	if len(rest) > 1 {
		dir = rest[1]
	}
	if entries, err := os.ReadDir(dir); err == nil && len(entries) > 0 && !force {
		return fmt.Errorf("%s is not empty; pass --force to overwrite files in it", dir)
	}
	req, _ := http.NewRequest(http.MethodGet, apiBase()+"/v1/nooks/"+name+"/pull", nil)
	req.Header.Set("Authorization", "Bearer "+token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var out map[string]any
		if json.NewDecoder(resp.Body).Decode(&out) != nil || out["error"] == nil {
			return fmt.Errorf("pull failed (%s)", resp.Status)
		}
		return fmt.Errorf("%v", out["error"])
	}
	gz, err := gzip.NewReader(resp.Body)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	n := 0
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		clean := filepath.Clean(hdr.Name)
		if strings.HasPrefix(clean, "..") || filepath.IsAbs(clean) {
			return fmt.Errorf("bad path in archive: %s", hdr.Name)
		}
		target := filepath.Join(dir, clean)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return err
		}
		if err := os.WriteFile(target, b, 0o644); err != nil {
			return err
		}
		n++
	}
	return emit(asJSON, map[string]any{"name": name, "dir": dir, "files": n}, fmt.Sprintf("pulled %s: %d files into %s/\nedit, then `cd %s && nook deploy`", name, n, dir, dir))
}

func remix(args []string, asJSON bool) error {
	as, args := flagValue(args, "--as")
	if len(args) < 1 {
		return fmt.Errorf("usage: nook remix <nook> [--as name]")
	}
	q := ""
	if as != "" {
		q = "?name=" + as
	}
	out, err := callJSON(http.MethodPost, "/v1/nooks/"+args[0]+"/remix"+q, map[string]string{})
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("remixed %v as %v (yours, no data)\n%v", out["from"], out["name"], out["url"]))
}

func exportNook(args []string, asJSON bool) error {
	outPath, args := flagValue(args, "-o")
	name, _, err := nookName(args, nil)
	if err != nil {
		return err
	}
	if outPath == "" {
		outPath = name + ".nook.tar.gz"
	}
	req, _ := http.NewRequest(http.MethodGet, apiBase()+"/v1/nooks/"+name+"/export", nil)
	req.Header.Set("Authorization", "Bearer "+token())
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		var out map[string]any
		if json.NewDecoder(resp.Body).Decode(&out) != nil || out["error"] == nil {
			return fmt.Errorf("export failed (%s); is the server up to date?", resp.Status)
		}
		return fmt.Errorf("%v", out["error"])
	}
	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	n, err := io.Copy(f, resp.Body)
	if err != nil {
		return err
	}
	return emit(asJSON, map[string]any{"name": name, "file": outPath, "bytes": n}, fmt.Sprintf("exported %s to %s (%s)", name, outPath, human(int(n))))
}

func importNook(args []string, asJSON bool) error {
	newName, args := flagValue(args, "--name")
	share := false
	var rest []string
	for _, a := range args {
		if a == "--share" {
			share = true
		} else {
			rest = append(rest, a)
		}
	}
	if len(rest) < 1 {
		return fmt.Errorf("usage: nook import <file.nook.tar.gz> [--name n] [--share]")
	}
	f, err := os.Open(rest[0])
	if err != nil {
		return err
	}
	defer f.Close()
	q := "?"
	if newName != "" {
		q += "name=" + newName + "&"
	}
	if share {
		q += "share=1"
	}
	out, err := call(http.MethodPost, "/v1/import"+q, f, "application/gzip")
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("imported %v (%v files, %v documents)\n%v", out["name"], out["files"], out["documents"], out["url"]))
}

func versions(args []string, asJSON bool) error {
	name, _, err := nookName(args, nil)
	if err != nil {
		return err
	}
	out, err := call(http.MethodGet, "/v1/nooks/"+name+"/versions", nil, "")
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	for _, x := range out["versions"].([]any) {
		v := x.(map[string]any)
		mark := " "
		if v["current"] == true {
			mark = "*"
		}
		fmt.Printf("%s v%-4v %v files  %v\n", mark, v["version"], v["files"], v["created_at"])
	}
	return nil
}

func rollback(args []string, asJSON bool) error {
	name, args, err := nookName(args, isNumber)
	if err != nil {
		return err
	}
	v := 0
	if len(args) > 0 {
		fmt.Sscanf(args[0], "%d", &v)
	}
	out, err := callJSON(http.MethodPost, "/v1/nooks/"+name+"/rollback", map[string]int{"version": v})
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("%s is now v%v\n%s", name, out["version"], out["url"]))
}

func list(asJSON bool) error {
	out, err := call(http.MethodGet, "/v1/nooks", nil, "")
	if err != nil {
		return err
	}
	if asJSON {
		return json.NewEncoder(os.Stdout).Encode(out)
	}
	nooks, _ := out["nooks"].([]any)
	if len(nooks) == 0 {
		fmt.Println("no nooks yet. run `nook deploy` in a folder with a nook.json.")
	}
	for _, x := range nooks {
		n := x.(map[string]any)
		grants, _ := n["grants"].([]any)
		fmt.Printf("%-20s v%-3v %-8v %d shared  %s\n", n["name"], n["version"], n["mode"], len(grants), n["url"])
	}
	if shared, _ := out["shared"].([]any); len(shared) > 0 {
		fmt.Println("\nshared with you:")
		for _, x := range shared {
			n := x.(map[string]any)
			fmt.Printf("%-20s %-8v by %-28v %s\n", n["name"], n["role"], n["owner"], n["url"])
		}
	}
	return nil
}

func share(args []string, asJSON bool) error {
	role, args := flagValue(args, "--role")
	name, args, err := nookName(args, isEmail)
	if err != nil {
		return err
	}
	if len(args) < 1 || !isEmail(args[0]) {
		return fmt.Errorf("usage: nook share [nook] <email> [--role viewer|editor]")
	}
	if role == "" {
		role = "viewer"
	}
	out, err := callJSON(http.MethodPost, "/v1/nooks/"+name+"/share", map[string]string{"email": args[0], "role": role})
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("shared %s with %s as %s\n%s", name, args[0], role, out["url"]))
}

func unshare(args []string, asJSON bool) error {
	name, args, err := nookName(args, isEmail)
	if err != nil {
		return err
	}
	if len(args) < 1 || !isEmail(args[0]) {
		return fmt.Errorf("usage: nook unshare [nook] <email>")
	}
	out, err := call(http.MethodDelete, "/v1/nooks/"+name+"/share/"+args[0], nil, "")
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("removed %s from %s", args[0], name))
}

func transfer(args []string, asJSON bool) error {
	name, args, err := nookName(args, isEmail)
	if err != nil {
		return err
	}
	if len(args) < 1 || !isEmail(args[0]) {
		return fmt.Errorf("usage: nook transfer [nook] <email>")
	}
	out, err := callJSON(http.MethodPost, "/v1/nooks/"+name+"/transfer", map[string]string{"email": args[0]})
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("%s now belongs to %s (you are an editor)\n%s", name, args[0], out["url"]))
}

func mode(args []string, asJSON bool) error {
	name, args, err := nookName(args, isMode)
	if err != nil {
		return err
	}
	if len(args) < 1 || !isMode(args[0]) {
		return fmt.Errorf("usage: nook mode [nook] <private|org|link|public>")
	}
	out, err := callJSON(http.MethodPut, "/v1/nooks/"+name+"/mode", map[string]string{"mode": args[0]})
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("%s is now %s\n%s", name, args[0], out["url"]))
}

func openNook(args []string, asJSON bool) error {
	name, _, err := nookName(args, nil)
	if err != nil {
		return err
	}
	out, err := call(http.MethodGet, "/v1/nooks/"+name, nil, "")
	if err != nil {
		return err
	}
	u := out["url"].(string)
	if !asJSON {
		openBrowser(u)
	}
	return emit(asJSON, out, u)
}

func deleteNook(args []string, asJSON bool) error {
	name, _, err := nookName(args, nil)
	if err != nil {
		return err
	}
	out, err := call(http.MethodDelete, "/v1/nooks/"+name, nil, "")
	if err != nil {
		return err
	}
	return emit(asJSON, out, fmt.Sprintf("deleted %s and its data", name))
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	cmd.Start()
}

// tarball gzips the directory, honoring .nookignore (one name or path prefix per line).
func tarball(dir string) (*bytes.Buffer, int, error) {
	// Agent and editor config never belongs in a deploy.
	ignore := []string{".git", ".nookignore", ".DS_Store", "node_modules", ".claude", ".cursor", ".vscode", ".idea", ".env"}
	if b, err := os.ReadFile(filepath.Join(dir, ".nookignore")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") {
				ignore = append(ignore, strings.TrimSuffix(l, "/"))
			}
		}
	}
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	count, total := 0, 0
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		for _, ig := range ignore {
			if rel == ig || strings.HasPrefix(rel, ig+"/") || d.Name() == ig {
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if total += len(b); total > maxDeployBytes {
			return fmt.Errorf("deploy exceeds %s", human(maxDeployBytes))
		}
		if err := tw.WriteHeader(&tar.Header{Name: rel, Mode: 0o644, Size: int64(len(b)), Typeflag: tar.TypeReg}); err != nil {
			return err
		}
		_, err = tw.Write(b)
		count++
		return err
	})
	if err != nil {
		return nil, 0, err
	}
	if err := tw.Close(); err != nil {
		return nil, 0, err
	}
	if err := gz.Close(); err != nil {
		return nil, 0, err
	}
	return &buf, count, nil
}

func human(n int) string {
	switch {
	case n >= 1<<20:
		return fmt.Sprintf("%.1f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f KB", float64(n)/(1<<10))
	}
	return fmt.Sprintf("%d B", n)
}
