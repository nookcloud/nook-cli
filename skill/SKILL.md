---
name: nook
description: Build and deploy a nook, a small web app that lives at a URL and is shared with specific people. Use when the user wants a personal or team tool put online, shared with someone, or when they mention Nook, getnook.dev, nook deploy, or nook.js.
---

# Building a nook

A **nook** is a small app that lives at `https://<name>.getnook.dev` and is shared with the
people its owner chooses. You write ordinary HTML/JS/CSS. Nook provides hosting, sign-in,
sharing, and a per-nook database you reach from the page through `nook.js`. There is no
backend to write and no login to implement.

## The three commands

```
nook login                  # once; opens the browser
nook deploy                 # in a folder with nook.json → prints the URL
nook share sam@example.com  # optional: --role editor
```

Starting from nothing? `nook init [name]` writes `nook.json`, a starter `index.html` that already
uses nook.js, and this file at `.claude/skills/nook/SKILL.md`. Then edit and deploy.

Every command accepts `--json`. Parse that instead of scraping text. `nook deploy --json` prints
`{"name","version","url","mode","files","warnings":[...]}`. Errors print `{"error":"..."}` with a
non-zero exit.

## Files a nook needs

```
nook.json      { "name": "expenses", "type": "static" }   name: 2-40 chars, a-z 0-9 -
index.html     the app; include <script src="/_nook/nook.js"></script>
```

Anything else in the folder (css, js, images) deploys too. Add a `.nookignore` (one path per
line) to skip files. Deploys are capped at 10 MB. `type` is `static` for now.

## Rules the page must follow

- Load the client from the nook's own origin: `<script src="/_nook/nook.js"></script>`.
- Content Security Policy, exactly: same-origin scripts, styles, fetches, fonts, and images;
  inline `<script>`, inline `<style>`, and inline `on*=` attributes allowed; `data:` and `blob:`
  URLs allowed for images only; **nothing** from other domains (no CDN scripts, no external
  CSS, no `fetch` to other hosts). The page cannot be framed.
- Never put API keys or secrets in the page. Everyone who can open the nook can read it.
- Do not implement login. The platform signs people in before the page loads.
- Escape user-supplied strings before putting them in `innerHTML`. Other viewers wrote them.
- Redeploying replaces the code and keeps the data.

## nook.js

```js
const user = await nook.ready;   // resolves with nook.user once the viewer is known
nook.user.email        // "sam@example.com", lowercase; null when anonymous
nook.user.name         // display name from Google, e.g. "Sam Lee"; may be null
nook.user.role         // "owner" | "editor" | "viewer" | null (anonymous on a public nook)
nook.user.canEdit      // true for owner and editor, false otherwise
nook.user.signedIn     // boolean
nook.user.owner        // the nook owner's email, always set
nook.user.nook         // the nook's name; nook.user.mode is its share mode
nook.signIn()          // send an anonymous viewer to sign in (public nooks)
```

Data lives in named collections of JSON objects (documents):

```js
const { documents, next } = await nook.data.list("expenses", {
  order: "desc",            // "asc" (default) or "desc" by created_at
  limit: 100,               // default 100, max 200
  cursor: next,             // pass the previous call's `next` for the next page; "" when done
  where: { paid: false }    // equality on top-level fields of `data`; keys are ANDed
});
const doc = await nook.data.create("expenses", { who: "sam", amount: 12.5 }); // → document
await nook.data.update("expenses", doc.id, { paid: true });   // merges fields → document
await nook.data.get("expenses", doc.id);                       // → document
await nook.data.remove("expenses", doc.id);                    // → { deleted: id }
nook.data.exportUrl("expenses", "csv")                         // relative URL for a link
```

A document is `{ id, collection, data, created_by, created_at, updated_at }`. `data` is the
object you stored (any JSON; numbers are doubles). `created_by` is the creator's lowercase
email. Timestamps are ISO 8601 UTC strings, so `new Date(d.created_at)` works. Listing an
empty collection returns `{ documents: [], next: "" }`, never an error.

Calls work before `nook.ready` resolves; awaiting it first just lets you know who is viewing.
Errors throw an `Error` with `.message` (the server's reason) and `.status`: 401 not signed in,
403 not allowed, 404 missing, 400 bad input.

**Shared vs personal.** A collection like `"expenses"` is shared: every viewer sees the same
documents. A collection prefixed `me/`, like `"me/settings"`, is personal: each viewer sees only
documents they created, and nobody else can read them. Use `me/` for preferences and drafts.
For names, use `nook.user.name`; it is already shared knowledge, so you do not need a profile
collection.

**Who can write.** Owners and editors create, update, and delete in shared collections.
Viewers read shared collections and can write to their own `me/` collections. Anonymous
viewers on public nooks can read shared collections and use `exportUrl`, nothing else. Check
`nook.user.canEdit` and hide controls that would fail.

**Freshness.** There is no push. Re-list after your own writes. If other people's changes must
appear without a reload, poll `list` every few seconds while the tab is visible.

**About and source.** Every nook has `/_nook/about` (owner, who can open it, what the code can
do, data, versions) and `/_nook/source/<file>` (the live source as text). Link to them if the
people you share with will want to know what they are using.

**The data table.** Every nook has a spreadsheet view at `/_nook/data` (link it from your page if
useful). Editors edit cells, add and delete rows, and import CSV there; viewers browse. The CLI
does the same: `nook data <nook> import <collection> file.csv` (header row = field names).

**CSV export** has one column per top-level field of `data` (union across documents, sorted),
plus `id`, `created_by`, `created_at`, `updated_at`. Nested values are JSON strings.

## Patterns

Escape before rendering:
```js
const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
```

One settings document per viewer (upsert in `me/`):
```js
const { documents } = await nook.data.list("me/settings", { limit: 1 });
const settings = documents[0]
  ? await nook.data.update("me/settings", documents[0].id, { theme: "dark" })
  : await nook.data.create("me/settings", { theme: "dark" });
```

Load everything:
```js
let all = [], cursor = "";
do { const r = await nook.data.list("items", { limit: 200, cursor }); all.push(...r.documents); cursor = r.next; } while (cursor);
```

## A minimal page

```html
<!doctype html>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1">
<title>expenses</title>
<style>body{font:16px system-ui;margin:2rem auto;max-width:28rem;padding:0 1rem}</style>
<h1>expenses</h1>
<p id="who"></p>
<form id="f" hidden><input id="note" placeholder="what" required><input id="amt" type="number" step="0.01" required><button>add</button></form>
<ul id="list"></ul>
<script src="/_nook/nook.js"></script>
<script>
  const $ = id => document.getElementById(id);
  const esc = s => String(s ?? "").replace(/[&<>"']/g, c => ({"&":"&amp;","<":"&lt;",">":"&gt;",'"':"&quot;","'":"&#39;"}[c]));
  async function render() {
    const { documents } = await nook.data.list("expenses", { order: "desc" });
    $("list").innerHTML = documents.map(d =>
      `<li>${esc(d.data.note)} — $${Number(d.data.amount).toFixed(2)} <small>${esc(d.created_by)}</small></li>`).join("");
  }
  (async () => {
    const u = await nook.ready;
    $("who").textContent = u.signedIn ? `${u.name || u.email} · ${u.role}` : "guest";
    $("f").hidden = !u.canEdit;
    $("f").onsubmit = async e => {
      e.preventDefault();
      await nook.data.create("expenses", { note: $("note").value, amount: Number($("amt").value) });
      $("f").reset(); render();
    };
    render();
  })();
</script>
```

## Sharing

```
nook share sam@example.com               # viewer
nook share sam@example.com --role editor # can add and change data
nook unshare sam@example.com
nook mode link      # anyone signed in with the link can view
nook mode org       # anyone at the owner's company domain (not gmail etc.)
nook mode public    # anyone, no sign-in, read-only data; shows a small "not verified" bar
nook mode private   # back to explicit shares only (default)
```

## Using Nook as a tool (MCP)

`nook mcp` runs an MCP server over stdio with tools for deploy, list, share, mode, rollback,
data, and this skill. Register it once and call the tools instead of the CLI:

```
claude mcp add nook -- nook mcp          # Claude Code
# Cursor / others: command "nook", args ["mcp"]
```

## Edit an existing nook

`nook pull <name>` downloads a nook's current files (anyone who can open it can pull it, since
the browser already sees them). Edit, then `nook deploy` from that folder; only the owner or an
admin can deploy over the original, so for someone else's nook, `nook remix` first.

## Remix

`nook remix <name> [--as newname]` copies a nook you can open into one you own, with its files
and none of its data. Public and link-mode nooks show a remix link. It is how a tool spreads:
someone shares theirs, you make it yours, then change it with your agent.

## Take it with you

```
nook export expenses                 # writes expenses.nook.tar.gz: files + every document + share settings
nook import expenses.nook.tar.gz     # restores it here as a new nook you own (--name to rename, --share to reapply grants)
```

A nook is a folder and a database file. The export works on getnook.dev and on any nookd you run
yourself, so nothing you build is stuck anywhere.

## Other commands

```
nook list                  nook versions            nook rollback [version]
nook open                  nook delete              nook account whoami|plan|upgrade|logout
```

Run these inside the nook's folder, or add `--nook <name>`.

## Plans

Free keeps 5 nooks, 3 people shared on each, and 100 MB across your nooks. Pro is $12/mo,
unlimited. Hitting a limit only blocks something new (a 6th nook, a 4th collaborator); nothing
already deployed or shared stops working. `nook account plan` shows usage; `nook account upgrade` opens checkout (the bare `nook plan` and `nook upgrade` still work).
A 402 error names the limit and the way out.

## When something fails

- `sign in first`: run `nook login`.
- `the name "x" is taken`: change `name` in nook.json.
- `nook.json: name ...`: names are lowercase letters, digits, hyphens, 2–40 chars.
- Data calls fail with 403: the viewer is not an editor. Share with `--role editor`.
- Data calls fail with 401 on a `me/` collection: the viewer is anonymous; call `nook.signIn()`.
- Deploy prints warnings about secrets or `eval`: fix them; the deploy still went through.
- `402`: a free-plan limit. The message says which one; delete something or `nook upgrade`.
