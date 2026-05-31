# EdCode Agent Guidelines

## Terminal Rendering (Raw Mode + ANSI)

When using `golang.org/x/term` `MakeRaw()`, `\n` only moves cursor down — it does NOT return to column 0. This causes progressive right-shift on multi-line output.

**Rule: always use `\r\n` for multi-line output in raw mode.**

### Pattern for full-screen redraw (interactive menus)

```go
fullRender := func(sel []bool, cur int) string {
    var out string
    out += "\033[H\033[J"          // cursor home + clear screen
    out += banner.Logo + "\r\n"    // logo with \r\n
    out += prompt + "\r\n"
    for i, item := range items {
        marker := " "
        if i == cur { marker = ">" }
        checkbox := "[ ]"
        if sel[i] { checkbox = "[x]" }
        out += fmt.Sprintf("  %s %s  %s\r\n", marker, checkbox, item)
    }
    return out
}

// Print initial state (before raw mode)
fmt.Print(fullRender(initialSelection, 0))

// Enter raw mode
fd := int(os.Stdin.Fd())
oldState, _ := term.MakeRaw(fd)
defer term.Restore(fd, oldState)

// Read keystrokes, redraw on change
for {
    // ... read key, detect change ...
    if changed {
        fmt.Print(fullRender(selected, cursor))
    }
}
```

### Logo rendering

For multi-line ASCII art stored as a raw string literal:

```go
logoLines := strings.Split(banner.Logo, "\n")
for _, line := range logoLines {
    out += line + "\r\n"
}
```

### When NOT to use \r\n

- Output printed **before** entering raw mode: bare `\n` works fine
- Output printed **after** exiting raw mode: bare `\n` works fine once `term.Restore()` is called
- Single-line output: `\r` is unnecessary

### Common mistakes

1. **Mixing `\n` and `\r\n`** in the same render — causes drift on lines after the first
2. **Using `\033[u\033[J` (save/restore cursor) after entering raw mode** — the saved cursor position becomes unreliable after `MakeRaw()`
3. **Printing the banner with ANSI color codes** (`\033[32m`) in configure UI — the escape codes appear as literal text and corrupt the layout. Use `banner.PrintNoColor()` or `banner.ForRawMode()` for configure screens.

## Project Structure

```
cmd/edcode/main.go        — CLI entrypoint
internal/app/app.go       — Top-level orchestrator (App)
internal/config/config.go — YAML config structs, DefaultPath()
internal/configure/       — Interactive provider/agent setup
internal/provider/        — OpenAI, Anthropic, registry, factory
internal/agent/           — EnhancedAgent with memory, context, skills
internal/tool/            — Built-in tools (read, write, edit, bash, glob, grep, web, skill) + MCP client
internal/middleware/      — Pre/post model/tool hooks
internal/session/         — Session management
internal/memory/          — 3-tier memory (working, session, long-term)
internal/ctxmgr/          — Context compaction, summarization
internal/enhance/         — Auto-enhancement engine
internal/skills/          — SKILL.md parser, discover, load
internal/banner/          — ASCII logo (green Print, no-color PrintNoColor)
Makefile                  — build, install (user-local), install-system (sudo), setup, configure, reset, uninstall, clean, reinstall
```

## Config & Data

- Config file: `edcode.yaml` (auto-generated, append/overwrite, never delete)
- Data dirs: `~/.edcode/`
- Makefile install: `make install` → `~/.local/bin/edcode` (no sudo); `sudo make install-system` → `/usr/local/bin/edcode`
- Reset: `make reset` or `edcode --reset` removes `edcode.yaml` and `~/.edcode/`

## Providers

- OpenAI, Anthropic, OpenRouter, Ollama, custom OpenAI-compatible
- Auto-discover models via `GET /models` API endpoint
- Provider/model routing: `provider/model` format (handles embedded slashes like `openrouter/anthropic/claude-sonnet-4-20250514`)
- Ollama models may not support tools — use OpenAI/Anthropic for tool-enabled agents

## Agents

- Mandatory agents: `default`, `planner`, `coder`, `reviewer`
- Each agent has: model, description, max_steps, temperature, tools, permission rules
- First-run auto-configure triggers when no config or no usable API keys
- Auth error message: clear guidance to run `--configure` or set env var

## Skills

- Discovery paths: `.skills/`, `skills/`, `~/.edcode/skills/`
- Format: `SKILL.md` files, discoverable, on-demand loading via `skill` tool
- Load/unload/search/list via `internal/skills/skills.go`

## MCP / CodeGraph

- MCP client: `internal/tool/mcp/client.go` (JSON-RPC 2.0 over stdio/HTTP)
- CodeGraph config in `edcode.yaml`:
  ```yaml
  mcp:
    codegraph:
      enabled: true
      type: local
      command: npx
      args: ["-y", "@colbymchenry/codegraph", "serve", "--mcp"]
  ```

## Build

```bash
export PATH="/opt/homebrew/bin:$PATH"
go build -o edcode ./cmd/edcode/
```
