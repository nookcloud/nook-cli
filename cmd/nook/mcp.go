package main

// `nook mcp`: a Model Context Protocol server over stdio, so agents can use Nook as a tool.
// Implements the subset every client needs: initialize, tools/list, tools/call, ping.

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type mcpTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

func obj(props map[string]any, required ...string) map[string]any {
	s := map[string]any{"type": "object", "properties": props}
	if len(required) > 0 {
		s["required"] = required
	}
	return s
}

func str(desc string) map[string]any { return map[string]any{"type": "string", "description": desc} }

var mcpTools = []mcpTool{
	{"nook_deploy", "Deploy a directory containing nook.json as a nook. Returns the URL. Redeploys keep data.", obj(map[string]any{"dir": str("directory to deploy (default: current)")})},
	{"nook_list", "List the user's nooks and nooks shared with them.", obj(map[string]any{})},
	{"nook_share", "Share a nook with an email as viewer or editor.", obj(map[string]any{"nook": str("nook name"), "email": str("email to share with"), "role": str("viewer (default) or editor")}, "nook", "email")},
	{"nook_unshare", "Remove someone's access to a nook.", obj(map[string]any{"nook": str("nook name"), "email": str("email to remove")}, "nook", "email")},
	{"nook_mode", "Set a nook's share mode: private, org, link, or public.", obj(map[string]any{"nook": str("nook name"), "mode": str("private|org|link|public")}, "nook", "mode")},
	{"nook_data_list", "List documents in a nook collection. Collections prefixed me/ are per-person.", obj(map[string]any{"nook": str("nook name"), "collection": str("collection name"), "limit": map[string]any{"type": "integer"}, "order": str("asc or desc")}, "nook", "collection")},
	{"nook_data_create", "Create a document (a JSON object) in a nook collection.", obj(map[string]any{"nook": str("nook name"), "collection": str("collection name"), "data": map[string]any{"type": "object", "description": "the document"}}, "nook", "collection", "data")},
	{"nook_data_update", "Merge fields into an existing document.", obj(map[string]any{"nook": str("nook name"), "collection": str("collection name"), "id": str("document id"), "data": map[string]any{"type": "object"}}, "nook", "collection", "id", "data")},
	{"nook_data_delete", "Delete a document.", obj(map[string]any{"nook": str("nook name"), "collection": str("collection name"), "id": str("document id")}, "nook", "collection", "id")},
	{"nook_rollback", "Roll a nook back to a previous version (default: the one before current).", obj(map[string]any{"nook": str("nook name"), "version": map[string]any{"type": "integer"}}, "nook")},
	{"nook_remix", "Copy a nook the user can open into one they own, with its files and none of its data.", obj(map[string]any{"nook": str("source nook name"), "name": str("name for the copy (optional)")}, "nook")},
	{"nook_pull", "Download a nook's current files into a directory so they can be edited and redeployed.", obj(map[string]any{"nook": str("nook name"), "dir": str("target directory (default: the nook name)")}, "nook")},
	{"nook_skill", "Return the Nook skill: how to build a nook page with nook.js.", obj(map[string]any{})},
}

func mcpServe() error {
	if fi, _ := os.Stdin.Stat(); fi != nil && fi.Mode()&os.ModeCharDevice != 0 {
		fmt.Fprintln(os.Stderr, "nook mcp is an MCP server: it speaks JSON-RPC on stdin/stdout for an agent client.\nregister it once with:  claude mcp add nook -- nook mcp\n(waiting for JSON on stdin; ctrl-c to quit)")
	}
	in := bufio.NewReader(os.Stdin)
	out := json.NewEncoder(os.Stdout)
	for {
		line, err := in.ReadBytes('\n')
		if err == io.EOF && len(strings.TrimSpace(string(line))) == 0 {
			return nil
		}
		if err != nil && err != io.EOF {
			return err
		}
		if strings.TrimSpace(string(line)) == "" {
			continue
		}
		var req rpcRequest
		if json.Unmarshal(line, &req) != nil {
			out.Encode(rpcResponse{JSONRPC: "2.0", Error: &rpcError{-32700, "parse error"}})
			continue
		}
		if req.ID == nil { // notification
			continue
		}
		res, rerr := mcpHandle(req)
		resp := rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: res}
		if rerr != nil {
			resp.Error = rerr
		}
		out.Encode(resp)
		if err == io.EOF {
			return nil
		}
	}
}

func mcpHandle(req rpcRequest) (any, *rpcError) {
	switch req.Method {
	case "initialize":
		return map[string]any{
			"protocolVersion": "2025-06-18",
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "nook", "version": version},
			"instructions":    "Nook deploys small apps to a URL and shares them like a doc. Call nook_skill first when building a page. Run `nook login` in a terminal if calls fail with 'sign in first'.",
		}, nil
	case "ping":
		return map[string]any{}, nil
	case "tools/list":
		return map[string]any{"tools": mcpTools}, nil
	case "tools/call":
		var p struct {
			Name string         `json:"name"`
			Args map[string]any `json:"arguments"`
		}
		json.Unmarshal(req.Params, &p)
		text, isErr := mcpCall(p.Name, p.Args)
		return map[string]any{"content": []map[string]any{{"type": "text", "text": text}}, "isError": isErr}, nil
	}
	return nil, &rpcError{-32601, "method not found: " + req.Method}
}

func argStr(a map[string]any, k string) string {
	if v, ok := a[k].(string); ok {
		return v
	}
	return ""
}

// mcpCall runs a tool and returns text for the model. Errors come back as text with isError set.
func mcpCall(name string, a map[string]any) (string, bool) {
	res := func(v any, err error) (string, bool) {
		if err != nil {
			return "error: " + err.Error(), true
		}
		b, _ := json.MarshalIndent(v, "", "  ")
		return string(b), false
	}
	switch name {
	case "nook_deploy":
		dir := argStr(a, "dir")
		if dir == "" {
			dir = "."
		}
		return res(deployDir(dir))
	case "nook_list":
		return res(call(http.MethodGet, "/v1/nooks", nil, ""))
	case "nook_share":
		role := argStr(a, "role")
		if role == "" {
			role = "viewer"
		}
		return res(callJSON(http.MethodPost, "/v1/nooks/"+argStr(a, "nook")+"/share", map[string]string{"email": argStr(a, "email"), "role": role}))
	case "nook_unshare":
		return res(call(http.MethodDelete, "/v1/nooks/"+argStr(a, "nook")+"/share/"+argStr(a, "email"), nil, ""))
	case "nook_mode":
		return res(callJSON(http.MethodPut, "/v1/nooks/"+argStr(a, "nook")+"/mode", map[string]string{"mode": argStr(a, "mode")}))
	case "nook_rollback":
		v, _ := a["version"].(float64)
		return res(callJSON(http.MethodPost, "/v1/nooks/"+argStr(a, "nook")+"/rollback", map[string]int{"version": int(v)}))
	case "nook_pull":
		args := []string{argStr(a, "nook"), "--force"}
		if d := argStr(a, "dir"); d != "" {
			args = []string{argStr(a, "nook"), d, "--force"}
		}
		if err := pull(args, true); err != nil {
			return "error: " + err.Error(), true
		}
		return "pulled " + argStr(a, "nook") + " into " + func() string {
			if d := argStr(a, "dir"); d != "" {
				return d
			}
			return argStr(a, "nook")
		}(), false
	case "nook_remix":
		q := ""
		if n := argStr(a, "name"); n != "" {
			q = "?name=" + n
		}
		return res(callJSON(http.MethodPost, "/v1/nooks/"+argStr(a, "nook")+"/remix"+q, map[string]string{}))
	case "nook_data_list", "nook_data_create", "nook_data_update", "nook_data_delete":
		return res(mcpData(name, a))
	case "nook_skill":
		resp, err := http.Get(apiBase() + "/skill.md")
		if err != nil {
			return "error: " + err.Error(), true
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		return string(b), false
	}
	return "unknown tool " + name, true
}

func mcpData(name string, a map[string]any) (map[string]any, error) {
	nook, coll := argStr(a, "nook"), argStr(a, "collection")
	base := nookURL(nook) + "/_nook/data/" + coll
	do := func(method, u string, body any) (map[string]any, error) {
		var rd io.Reader
		if body != nil {
			b, _ := json.Marshal(body)
			rd = strings.NewReader(string(b))
		}
		req, err := http.NewRequest(method, u, rd)
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
	switch name {
	case "nook_data_list":
		q := "?"
		if o := argStr(a, "order"); o != "" {
			q += "order=" + o + "&"
		}
		if l, ok := a["limit"].(float64); ok && l > 0 {
			q += fmt.Sprintf("limit=%d", int(l))
		}
		return do(http.MethodGet, base+q, nil)
	case "nook_data_create":
		return do(http.MethodPost, base, a["data"])
	case "nook_data_update":
		return do(http.MethodPatch, base+"/"+argStr(a, "id"), a["data"])
	case "nook_data_delete":
		return do(http.MethodDelete, base+"/"+argStr(a, "id"), nil)
	}
	return nil, fmt.Errorf("unknown data tool")
}
