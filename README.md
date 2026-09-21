# nook

Deploy a small app to a URL with one command. Share it like a doc.

A **nook** is a small app that lives at `https://<name>.getnook.dev` and is shared with the
people you choose. You write ordinary HTML, JS, and CSS. Nook adds the URL, sign-in, sharing,
and a database your page reaches through one script tag.

```
curl -fsSL https://getnook.dev/install.sh | sh
nook login
nook init my-tool && nook deploy       # https://my-tool.getnook.dev
nook share my-tool sam@example.com     # --role editor to let them change data
```

This repo is the command-line tool, the MCP server (`nook mcp`), the browser client
(`nookjs/nook.js`), the manifest format, the agent skill (`skill/SKILL.md`), and example nooks.

## Build from source

```
go install github.com/nookcloud/nook-cli/cmd/nook@latest
```

## Use it from an agent

```
claude mcp add nook -- nook mcp
```

Then ask for a tool in plain words. `skill/SKILL.md` teaches any agent how to build a nook page.

## Examples

Each folder in `examples/` deploys as-is: `nook deploy examples/todo`.

| example | shows |
|---|---|
| hello | the smallest nook |
| todo | a shared list with editor and viewer roles |
| expenses | an expense splitter with a per-person `me/` collection |
| pool | a prediction pool with private picks and a shared leaderboard |
| invoice | invoices a freelancer shares with clients by link |

## License

MIT.
