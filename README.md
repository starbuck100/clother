<div align="center">
  <img src="docs/logo.png" alt="Clother logo" width="220" />
  <h1>Clother</h1>
  <p><strong>One CLI to switch between Claude Code providers instantly.</strong></p>
  <p>
    <a href="LICENSE"><img src="https://img.shields.io/badge/License-MIT-blue.svg" alt="MIT License" /></a>
    <a href="https://go.dev/"><img src="https://img.shields.io/badge/Language-Go-00ADD8.svg" alt="Go" /></a>
    <a href="#platform-support"><img src="https://img.shields.io/badge/Platform-macOS%20%7C%20Linux%20%7C%20Windows-blue.svg" alt="Platform macOS, Linux and Windows" /></a>
    <a href="https://github.com/jolehuit/clother/stargazers"><img src="https://img.shields.io/github/stars/jolehuit/clother?style=social" alt="GitHub stars" /></a>
  </p>
</div>

<br/>

<div align="center">
  <img src="docs/demo-fast.gif" alt="Clother terminal demo" width="900" />
</div>

> [!IMPORTANT]
> **This is the [`starbuck100` fork](https://github.com/starbuck100/clother) — it is where native Windows support lives.**
>
> Upstream [`jolehuit/clother`](https://github.com/jolehuit/clother) supports macOS and Linux only;
> on Windows it expects WSL. This fork adds a native Windows build that runs in `cmd.exe` and
> PowerShell with no WSL, no MSYS and no Go toolchain:
>
> - PowerShell installer — `scripts/install.ps1`
> - `clother-*.exe` launchers created with NTFS hardlinks (copy fallback on FAT32 / network shares)
> - Config overlays via NTFS junctions instead of symlinks — no admin rights, no Developer Mode
> - A `claude.exe` shim that survives Claude Code's own in-place updater
>
> **Windows install (PowerShell):**
>
> ```powershell
> irm https://raw.githubusercontent.com/starbuck100/clother/main/scripts/install.ps1 | iex
> ```
>
> This fork also adds live OpenRouter model selection and a direct Kilo gateway bridge. See
> [Platform Support](#platform-support) for the detail.

## Why Clother?

Switching Claude Code providers usually means changing env vars, endpoints, models, and launcher scripts by hand.
Clother gives you one install and one command pattern across Claude, Z.AI, Kimi, Alibaba, OpenRouter, local backends, China endpoints, and many other Anthropic-compatible providers.

## Table of Contents

- [Installation](#installation)
  - [PowerShell (Windows)](#powershell-windows)
- [Core Usage](#core-usage)
  - [Launching Claude Code](#launching-claude-code)
  - [Switching provider inside a session](#switching-provider-inside-a-session)
  - [Commands](#commands)
  - [Benchmarking](#benchmarking)
- [Provider Reference](#provider-reference)
- [Troubleshooting](#troubleshooting)
- [VS Code Integration](#vs-code-integration)
- [Platform Support](#platform-support)
- [Under the Hood](#under-the-hood)
- [Contributors](#contributors)
- [Star History](#star-history)
- [License](#license)

## Installation

### Homebrew (macOS recommended)

```bash
# 1. Install Claude Code CLI
curl -fsSL https://claude.ai/install.sh | bash

# 2. Install Clother via tap
brew tap jolehuit/tap
brew install clother

# 3. Start using it — all launchers are ready immediately
clother-native                          # Use your Claude Pro/Max/Team subscription
clother-zai                             # Z.AI (GLM-5.2)
clother-zai --yolo                      # Skip permission prompts
clother-kimi                            # Kimi (K3)
clother config                          # Configure providers
```

All `clother-*` provider launchers are installed directly into `$(brew --prefix)/bin` by the formula — no extra setup needed. `brew upgrade clother` keeps everything up to date.

**Update:**
```bash
clother update          # routes to brew upgrade under Homebrew
# or equivalently:
brew upgrade clother
```

### curl (macOS / Linux)

```bash
# 1. Install Claude Code CLI
curl -fsSL https://claude.ai/install.sh | bash

# 2. Install Clother
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/scripts/install.sh | bash

# 3. Start using it
clother-native                          # Use your Claude Pro/Max/Team subscription
clother-zai                             # Z.AI (GLM-5.2)
clother-zai --yolo                      # Skip permission prompts
clother-kimi                            # Kimi (K3)
clother-ollama --model qwen3-coder      # Local with Ollama
clother config                          # Configure providers
```

**Update:**
```bash
clother update          # downloads and installs latest release
```

This installs:
- `clother`
- `clother-*` provider launchers
- resume compatibility for `claude --resume ...`

### PowerShell (Windows)

Native Windows — no WSL, no Go toolchain. In PowerShell, two lines:

```powershell
# 1. Install Claude Code CLI — skip this if `claude` is already on your PATH
irm https://claude.ai/install.ps1 | iex

# 2. Install Clother
irm https://raw.githubusercontent.com/starbuck100/clother/main/scripts/install.ps1 | iex
```

From `cmd.exe` the same commands each need a PowerShell in front of them,
because `irm` is a PowerShell cmdlet and cmd does not have one — typing `irm`
into cmd gets you `'irm' is not recognized as an internal or external command`:

```bat
powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://claude.ai/install.ps1 | iex"

powershell -NoProfile -ExecutionPolicy Bypass -Command "irm https://raw.githubusercontent.com/starbuck100/clother/main/scripts/install.ps1 | iex"
```

`-NoProfile` keeps your PowerShell profile out of the installer's way.
`-ExecutionPolicy Bypass` lets the piped script run on machines whose policy
would otherwise refuse it.

```bat
:: 3. Start using it
clother-native                          :: Use your Claude Pro/Max/Team subscription
clother-zai                             :: Z.AI (GLM-5.2)
clother-zai --yolo                      :: Skip permission prompts
clother-kimi                            :: Kimi (K3)
clother-ollama --model qwen3-coder      :: Local with Ollama
clother config                          :: Configure providers
```

**Update:**
```bat
clother update          :: downloads and installs latest release
```

The Windows build uses NTFS hardlinks for `clother-*.exe`, so a launcher is the
same file as `clother.exe` rather than a second copy of it. On a filesystem that
has no hardlinks — FAT32, or a network share — it falls back to copying.

#### The `claude` shim on Windows

Like on macOS and Linux, the installer puts a `claude` on your `PATH` that hands
off to Clother, and moves your existing Claude Code aside to `claude-real`. That is
deliberate and reversible:

- `clother uninstall` puts `claude-real` back as `claude`. If you installed Claude
  Code again in the meantime, Clother leaves *your* newer binary alone.
- Skip the shim entirely with `-SkipClaudeShim` and keep working through
  `clother-*.exe` only:

```powershell
.\scripts\install.ps1 -SkipClaudeShim
```

On Windows the shim is a copy rather than a hardlink. Claude Code's own updater
rewrites `claude.exe` in place, and a hardlink would make that rewrite clobber
`clother.exe` with it.

### Installing a specific version, and updating

Both installers take the newest release by default. `CLOTHER_VERSION` pins a tag instead, which is also how a pre-release is installed:

```powershell
$env:CLOTHER_VERSION = 'v3.2.0-rc1'
irm https://raw.githubusercontent.com/starbuck100/clother/v3.2.0-rc1/scripts/install.ps1 | iex
```

```bash
CLOTHER_VERSION=v3.2.0-rc1 \
  curl -fsSL https://raw.githubusercontent.com/starbuck100/clother/v3.2.0-rc1/scripts/install.sh | bash
```

Re-running either script is also how you update: it installs the newest release over what is there, refreshing the launchers and the `/clother:*` commands along with the binary. Inside Clother, `clother update` does the same thing.

A pre-release never becomes `latest`, so `clother update` and the plain one-liner keep offering the newest stable release until a version without a hyphen in its tag is published.

### Install Options

By default, Clother installs launchers to:
- the same directory as your existing `claude` binary, when `claude` is already on `PATH`
- otherwise **macOS**: `~/bin`
- otherwise **Linux**: `~/.local/bin` (XDG standard)
- otherwise **Windows**: `%USERPROFILE%\bin`

If the chosen bin directory is not on `PATH`, `clother install` prints a warning with the exact directory to add.

You can override this with `--bin-dir` or the `CLOTHER_BIN` environment variable:

```bash
# Using --bin-dir flag
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/scripts/install.sh | bash -s -- --bin-dir ~/.local/bin

# Using environment variable
export CLOTHER_BIN="$HOME/.local/bin"
curl -fsSL https://raw.githubusercontent.com/jolehuit/clother/main/scripts/install.sh | bash
```

```powershell
# Windows: -BinDir is passed on to `clother install`, and any further
# arguments are forwarded verbatim
.\scripts\install.ps1 -BinDir "$env:USERPROFILE\bin"

# or through the environment, which is also the way to do it for the
# one-liner, where there is no script to pass a parameter to
$env:CLOTHER_BIN = "$env:USERPROFILE\bin"
```

Clother keeps `claude --resume ...` working with Clother features after install.

## Core Usage

### Launching Claude Code

`clother` on its own starts Claude Code under the provider you last chose:

```bash
clother                                   # the remembered provider, native on first run
clother --yolo                            # same, skipping permission prompts
clother --resume <id>                     # same, continuing a session
clother zai                               # switch to Z.AI, and remember it
clother zai --model glm-4.7               # ... with a different model
clother openrouter moonshotai/kimi-k2.6   # any OpenRouter model, by its tag
clother fix the bug in src/foo.go         # no provider named: the words go to Claude Code
clother -- --verbose                      # after --, everything belongs to Claude Code
```

The choice is remembered in `clother`'s own configuration (`clother status` prints it, `clother list` marks it with `(active)`). The `clother-<provider>` launchers still work exactly as they always have, and do not change what is remembered.

Everything after the provider name goes to Claude Code unchanged. Where the two readings collide — `clother zai` against `clother fix the bug` — the provider name wins, because `clother zai --resume <id>` has to work; `--` is how you say "this is Claude Code's". A mistyped command is reported with a suggestion instead of being passed on as a prompt.

For OpenRouter, enter a `vendor/model` tag from the current catalog. `clother config openrouter` fetches a live numbered list of free models compatible with text and tool calling; paid catalog models can also be entered directly. A model tag is only read for providers whose models are not a fixed list — `clother zai src/main.go` stays a prompt.

### Switching provider inside a session

Claude Code reads its endpoint and credentials once, at startup, so a session that is already running cannot be moved to another provider. Inside a session, `/clother:provider` therefore remembers the choice and prints the line that continues the same session under it:

```
/clother:provider zai       switch, and print how to continue
/clother:provider           what is remembered, and where this session is running
/clother:config             what the current provider resolves to, and how to change it
/clother:status             installation state
```

Run the printed `clother <provider> --resume <id>` line in your shell and the conversation continues on the new provider.

`clother install` writes those three commands into your Claude configuration directory, under `commands/clother/` — that directory is what namespaces them as `/clother:*`. They work in any Claude Code session, including ones Clother did not start, and `clother uninstall` removes them again. `clother install --no-commands` skips writing them.

### Commands

| Command | Description |
|---------|-------------|
| `clother config [provider]` | Configure provider |
| `clother list` | List profiles |
| `clother info <provider>` | Show provider details |
| `clother test` | Test connectivity |
| `clother bench [provider...] [--prompt "..."]` | Benchmark provider latency |
| `clother status` | Installation status |
| `clother install` | Install/update Clother (create/refresh symlinks) |
| `clother update` | Update to latest version |
| `clother uninstall` | Remove everything |

### Update

```bash
clother update
```

Routes to `brew upgrade clother` under Homebrew, or downloads the latest release for curl installs. Also refreshes provider symlinks.

### Changing the Default Model

Each provider launcher comes with a default model (for example `glm-5.2` for Z.AI). You can override it in two ways:

```bash
# One-time: pass --model through to Claude CLI
clother-zai --model glm-4.7

# Permanent: configure the provider and pick a different default
clother config zai
```

Use `clother info <provider>` to inspect the resolved model.

### Benchmarking

Compare latency across all configured providers at once:

```bash
clother bench                              # all configured providers
clother bench zai kimi                     # specific providers only
clother bench --prompt "Write a haiku"     # custom prompt
```

Output shows **TTFT** (time to first token) and total response time, sorted fastest first:

```
  Provider           Model                      TTFT    Total   Preview
  ──────────────────────────────────────────────────────────────────────────────
  kimi               k3-256k                    180ms    0.9s   "Hello!"
  zai                glm-5.2                    312ms    1.2s   "Hello!"
  deepseek           deepseek-chat              890ms    3.1s   "Hello!"
```

Only providers with a configured API key are included. Local providers are skipped.

### Resume

Clother keeps the resume command printed by Claude Code working across providers.

After a provider-launched session, Clother also prints a provider-aware reopen
command such as:

```bash
clother-kimi --resume <session-id>
```

When resuming a non-Claude session into native Claude, Clother temporarily
sanitizes incompatible non-Claude thinking blocks for the duration of that
single launch, then restores the original session file afterwards.

## Kilo free models (no Kilo Code installation)

```powershell
clother update
clother config kilo
clother-kilo --yolo
```

Configuration fetches Kilo's current catalog and lists free models that advertise text
and tool support, with numbered selection, context/output limits and the provider's
training flag. Leave the optional key empty for anonymous use. A saved Kilo key can
be removed by entering `-`. Paid models require a Kilo gateway key.

The default is `kilo-auto/free`. You can also choose an explicit catalog model:

```powershell
clother kilo --model stealth/space-bunny-alpha --yolo
```

Clother talks directly to [Kilo Gateway](https://kilo.ai/docs/gateway) and translates
Claude's Messages API to its Chat Completions API, including streamed text, tool calls
and tool results. No Kilo Code app, extension or CLI is needed. Claude Code itself is
still required. Anonymous use is subject to Kilo's free-model quotas and availability.

Catalog data is refreshed during configuration and cached for at most 15 minutes at
launch. Context and output limits bound Claude's settings; each request also caps output
for its selected model. Smaller user limits are preserved. Free pricing is checked from
the catalog, including numeric zero prices; anonymous requests cannot use paid models.

Text and ordinary client/MCP tools are supported, as are message images when advertised
by the model. PDF/document blocks, image tool results, native Anthropic server tools and
exact token counting are not currently supported by this bridge and return explicit
errors. Tool search is disabled for Kilo sessions so tools are sent as normal definitions.
Incomplete tool streams produce an error before any buffered tool is handed to Claude.

`clother test kilo` checks catalog reachability and model compatibility; it does not
claim to test authentication or inference. `clother bench kilo` sends a real model request.

## Usage, free quotas and fallback skill

```powershell
clother usage
clother usage openrouter --json
clother usage next kilo stealth/space-bunny-alpha
clother usage next openrouter cohere/north-mini-code:free --json
```

From v3.5.0, new OpenRouter and Kilo sessions and gateway benchmarks record local
request counts, provider-reported tokens/cache/costs and limit observations. The
ledger contains no prompts, generated text, tool arguments or credentials. It uses
immutable per-request files under the Clother data directory, `usage/v1`, to support
simultaneous sessions. The CLI displays today (UTC), the last hour and 30 days.
Older metadata stays on disk until you remove that directory. Usage before tracking
was installed and traffic outside Clother cannot be reconstructed.

OpenRouter's live `/api/v1/key` response supplies the daily free-request policy
(`used`, `limit`, `remaining`), resetting at midnight UTC. Missing fields are unknown,
not zero; credit balances are separate. Exempt endpoints/accounts and BYOK may not
be gated by the reported policy. Kilo documents 200 free requests/hour/IP across
models and keys, but no exact remaining-counter API. Clother therefore shows local
observations and confirmed limit errors, never `200 - local requests` as a claimed
remaining balance. `Retry-After`/reset headers are used when supplied. A generic
429 is kept distinct from a confirmed provider-wide free limit or upstream limit.

`usage next` refreshes **both** model catalogs, excludes paid/incompatible choices,
blocks every model of an exhausted provider and considers context, image support,
the same model on another gateway and recent local success. Its ranking is a
capability heuristic, not a quality benchmark or live inference test. Context is
estimated from recent reported usage with a reserve, not exact tokenization of a
future request. It recommends commands; it does not switch or replay tool calls.

`usage next` also includes **LiveBench** scores across Coding, Agentic Coding,
Reasoning, Mathematics, Data Analysis, Language and instruction following. It
fetches the current public dataset, caches it for one hour and reports dataset
version, source modification time and fetch time separately. All eligible live
models are listed. Exact model versions must match; stealth/unknown models stay
unranked. Missing scores are not zero. The installed **clother-free-fallback** skill
also explains when Arena's text/vision/web-development categories or Artificial
Analysis are useful additional sources, without conflating their scales.

New free-model sessions prepare a fallback order with **the same starting model**
(one tracked request, 18-second timeout). Invalid/unavailable planning falls back
to deterministic capability/benchmark ordering. The local router keeps the same
Claude Code process and history, rechecks quotas, tries compatible free models
across both gateways, clamps output limits, and displays backend switches in the
response. `/clother:usage` shows the active backend even when Claude's static model
label still shows the original. No request is replayed after content/tool output
starts; transport failures with unknown completion are not replayed either.

Paid fallback is opt-in:

```powershell
clother config fallback paid   # free first, then configured API keys (may cost)
clother config fallback free   # free only (default)
```

The preferred keyed fallback is DeepSeek V4.1 Flash (`deepseek-flash`), followed by
other configured Anthropic-compatible profiles. Credentials are never invented.
Unknown model capacities use a conservative 32k text bound; long/image requests
may have no eligible replacement. Free/provider quota failures skip the entire
provider. Up to six rejected attempts are allowed; schema errors and partial
streams stop instead of cycling. Quota checks cannot guarantee future availability.

Set `CLOTHER_FALLBACK_CATEGORY` to a LiveBench category (default `Agentic Coding`),
`CLOTHER_FALLBACK_PLANNER=0` to skip startup inference, `CLOTHER_BENCHMARKS=0` to skip
benchmark fetches, or `CLOTHER_AUTO_FALLBACK=0` to disable session routing.
Installation adds `/clother:usage` and `/clother-free-fallback` to Claude Code.

For an upgrade from v3.4.0 or older, refresh generated files once after updating:

```powershell
clother update
clother install
```

The older updater replaces the executable using its old list of generated files;
the second command installs the new usage command and skill. Existing provider
settings and keys are preserved. `--no-commands` skips generated commands/skills.

Primary policies: [OpenRouter limits](https://openrouter.ai/docs/api/reference/limits)
and [Kilo usage and billing](https://kilo.ai/docs/gateway/usage-and-billing).

## Provider Reference

### Cloud

| Command | Provider | Model | API Key |
|---------|----------|-------|---------|
| `clother-native` | Anthropic | Claude | Your subscription |
| `clother-zai` | Z.AI | GLM-5.2 | [z.ai](https://z.ai) |
| `clother-minimax` | MiniMax | MiniMax-M3 | [minimax.io](https://minimax.io) |
| `clother-kimi` | Kimi | k3-256k | [kimi.com](https://kimi.com) |
| `clother-moonshot` | Moonshot AI | kimi-k3 | [moonshot.ai](https://moonshot.ai) |
| `clother-deepseek` | DeepSeek | deepseek-flash | [deepseek.com](https://platform.deepseek.com) |
| `clother-mimo` | Xiaomi MiMo | mimo-v2.5-pro | [xiaomimimo.com](https://platform.xiaomimimo.com) |
| `clother-alibaba` | Alibaba Coding Plan | qwen3.7-plus | [modelstudio](https://modelstudio.console.alibabacloud.com) |
| `clother-alibaba-us` | Alibaba Coding Plan (US) | qwen3.7-plus | [modelstudio](https://modelstudio.console.alibabacloud.com) |

### OpenRouter (100+ Models)

```bash
clother config openrouter               # Set API key, default model, optional aliases
clother-openrouter --yolo               # Launch with the configured default model
clother-openrouter --yolo --model vendor/model
clother test openrouter                 # Check authentication without inference charges
# Example: alias moonshotai/kimi-k2.5 as kimi-k25
clother-or kimi-k25                     # Works on every install
clother-or-kimi-k25                     # Per-alias shortcut (curl installs)
```

`clother-openrouter` works without creating an alias. `clother config openrouter`
fetches the public model catalog and lists currently free models with text and
tool support, including context and output limits. Pick a number for the default
or an alias, or enter any compatible catalog model ID (including paid models).
Models with missing/nonzero/conditional prices are not advertised as free.

At launch, Clother checks the effective model (including `--model` overrides and
aliases), using a catalog cache valid for 15 minutes. It bounds Claude Code's
context, output and thinking settings and writes them to the temporary settings
overlay. Output is at most the advertised ceiling, 32,000 tokens, and one quarter
of the context window; a missing output ceiling uses a conservative 4,096-token
cap. Smaller user limits are preserved. Thinking is disabled when unsupported.
Use a current Claude Code version that supports `CLAUDE_CODE_MAX_CONTEXT_TOKENS`.
Unknown or incompatible models produce an actionable error before launch.

For `qwen/qwen3.8-27b` and its `:free` variant, Clother runs a temporary,
authenticated loopback proxy to work around the provider's rejection of
`minLength` in tool schemas (for example `artifact.favicon`). Only outbound tool
schemas are adjusted: minimum lengths become description hints, while property
names, types, required fields, prompts and tool results remain intact. The
original tool definitions stay in Claude/MCP; server-side grammar enforcement of
the minimum length is unavailable for this provider. Other models are unchanged.
The proxy forwards streaming responses and closes when the launcher exits.

Free pricing does not remove provider quotas, rate limits or availability errors.
The public catalog is not a per-provider capacity guarantee. A very large prompt
or a model change inside Claude Code can still need a new session or relaunch;
Clother applies these limits to the model selected **at launch**.

Clother uses OpenRouter's [Anthropic-compatible endpoint](https://openrouter.ai/docs/cookbook/coding-agents/claude-code-integration),
with bearer authentication and an explicitly empty Anthropic API key. Main,
Fable, Haiku, Sonnet, Opus, small/fast and subagent roles use the selected model.
Claude Code compatibility depends on the model and upstream provider; OpenRouter
only guarantees full compatibility with Anthropic's first-party provider.

`clother test openrouter` checks the configured key with
[`GET /api/v1/key`](https://openrouter.ai/docs/api/api-reference/api-keys/get-current-key).
Missing keys, authentication failures and invalid responses return a nonzero
exit code. This does not prove model availability or inference; use
`clother-openrouter --print "Reply with OK"` for a real, billable model request.

`clother-or <alias>` works on every install. curl and Windows installs additionally get a
`clother-or-<alias>` symlink per alias when you run `clother config`; Homebrew
installs skip per-alias symlinks (the formula owns its bin directory), so use
the `clother-or <alias>` form there.

> **Tip**: Find model IDs on [openrouter.ai/models](https://openrouter.ai/models) — click the copy icon next to any model name.

> If a model doesn't work as expected, try the `:exacto` variant (e.g. `moonshotai/kimi-k2-0905:exacto`) which provides better tool calling support.

### China Endpoints

| Command | Provider | Endpoint |
|---------|----------|----------|
| `clother-zai-cn` | Z.AI China | open.bigmodel.cn |
| `clother-minimax-cn` | MiniMax China | api.minimaxi.com |
| `clother-ve` | Volcengine | ark.cn-beijing.volces.com |
| `clother-alibaba-cn` | Alibaba China | coding.dashscope.aliyuncs.com |

### Local (No API Key)

| Command | Provider | Port | Setup |
|---------|----------|------|-------|
| `clother-ollama` | Ollama | 11434 | [ollama.com](https://ollama.com) |
| `clother-lmstudio` | LM Studio | 1234 | [lmstudio.ai](https://lmstudio.ai) |
| `clother-llamacpp` | llama.cpp | 8000 | [github.com/ggml-org/llama.cpp](https://github.com/ggml-org/llama.cpp) |

```bash
# Ollama
ollama pull qwen3-coder && ollama serve
clother-ollama --model qwen3-coder

# LM Studio
clother-lmstudio --model <model>

# llama.cpp
./llama-server --model model.gguf --port 8000 --jinja
clother-llamacpp --model <model>
```

#### Remote servers

Local launchers default to `localhost`, but the backend can run on another
machine. Point a launcher at a remote base URL with `clother config`:

```bash
clother config lmstudio
# Base URL [http://localhost:1234]: http://192.168.123.123:1234
clother-lmstudio --model <model>
```

Works the same for `ollama` and `llamacpp`. Enter the default localhost URL
again to switch back.

### Custom

Any Anthropic-compatible endpoint:

```bash
clother config custom                   # e.g. name it "myprovider"
clother-custom myprovider               # Works on every install
clother-myprovider                      # Per-provider shortcut (curl installs)
```

As with OpenRouter aliases, the per-provider `clother-<name>` symlink is only
created on curl installs — under Homebrew, use `clother-custom <name>`.

### Alibaba Coding Plan Models

All Alibaba variants (`alibaba`, `alibaba-us`, `alibaba-cn`) share the same API key. The Coding Plan enforces an exact-string model allowlist; the currently supported IDs are:

| Model |
|-------|
| `qwen3.7-plus` (default) |
| `qwen3.6-plus` |
| `qwen3.5-plus` |
| `kimi-k2.5` |
| `glm-5` |
| `MiniMax-M2.5` |
| `qwen3-coder-next` |
| `qwen3-coder-plus` |
| `qwen3-max-2026-01-23` |
| `glm-4.7` |

Switch models with `--model`:

```bash
clother-alibaba --model kimi-k2.5
clother-alibaba --model glm-5
clother-alibaba-cn --model qwen3-coder-next
```

## Troubleshooting

| Problem | Solution |
|---------|----------|
| `claude: command not found` | Install Claude CLI first |
| `clother: command not found` | Run `clother status` to see the installed bin dir, then add that directory to `PATH` and restart your shell |
| `claude --resume ...` does not behave like Clother | Restart your shell, then run `clother install` again |
| `--yolo` is not recognized | Restart your shell, then run `clother install` again |
| `API key not set` | Run `clother config` |

## VS Code Integration

Clother works with the official **Claude Code** extension.
Use Claude Code extension `2.6+`.

To configure it:

1. Open VS Code Settings (`Cmd+,` or `Ctrl+,`).
2. Search for **"Claude Process Wrapper"** (`claudeProcessWrapper`).
3. Set it to the **full path** of your chosen launcher:
   - macOS: `/Users/yourname/bin/clother-zai`
   - Linux: `/home/yourname/.local/bin/clother-zai`
   - Windows: `C:\Users\yourname\bin\clother-zai.exe`
4. Reload VS Code.

> **Note**: Requires Clother v2.6+ (which handles non-interactive shell output correctly).

## Platform Support

| Platform | Support | Where |
|----------|---------|-------|
| macOS (zsh/bash) | Full | Upstream + this fork |
| Linux (zsh/bash) | Full | Upstream + this fork |
| Windows 10/11 (cmd.exe, PowerShell) | Full, native — no WSL, no MSYS | **This fork only** |
| Windows under WSL | Works, but not required | Upstream + this fork |

Native Windows support is the reason this fork exists. Clother itself is a single Go binary
and always compiled for Windows; what upstream lacked was the runtime plumbing — symlinks,
`/dev/tty`, the `claude` lookup, and Windows release assets. The fork supplies those in a
per-OS `internal/platform` package, so Unix behaviour is untouched and upstream parity is
maintained for every other platform. See [PowerShell (Windows)](#powershell-windows) for install
and [The `claude` shim on Windows](#the-claude-shim-on-windows) for the one Windows-specific
behaviour worth knowing about.

Windows binaries are published on the [fork's releases](https://github.com/starbuck100/clother/releases)
as `clother_windows_amd64.zip` and `clother_windows_arm64.zip`, and CI runs a test job on
`windows-latest` so the Windows path does not drift silently.

## Under the Hood

### How It Works

Clother is a single Go binary. The installer downloads the release artifact,
installs `clother` into your bin directory, then creates:
- `clother-*` symlinks for providers
- a `claude` shim symlink for resume compatibility

At runtime, the binary resolves the selected profile from its own invocation
name, loads config and secrets, sets the required Anthropic-compatible
environment variables, then launches the real Claude binary outside the Clother
bin directory.

Example for `clother-zai`:

```bash
export ANTHROPIC_BASE_URL="https://api.z.ai/api/anthropic"
export ANTHROPIC_AUTH_TOKEN="$ZAI_API_KEY"
exec /path/to/the/real/claude "$@"
```

API keys stored in `~/.local/share/clother/secrets.env` (chmod 600).

`--yolo` is accepted by Clother launchers and by the Clother `claude` shim as
shorthand for `--dangerously-skip-permissions`.

### Local Release Testing

Test the binary installer locally against a local directory or server:

```bash
CLOTHER_RELEASE_BASE_URL=http://127.0.0.1:8000 \
  ./scripts/install.sh install
```

```powershell
$env:CLOTHER_RELEASE_BASE_URL = 'http://127.0.0.1:8000'
.\scripts\install.ps1 install
```

## Contributors

- [@darkokoa](https://github.com/darkokoa) — China endpoints
- [@RawToast](https://github.com/RawToast) — Kimi endpoint fix
- [@sammcj](https://github.com/sammcj) — Security hardening
- [@aprakasa](https://github.com/aprakasa) — Linux compatibility fixes in `load_secrets()`
- [@luciano-fiandesio](https://github.com/luciano-fiandesio) — Install directory improvement (issue)
- [@canberksinangil](https://github.com/canberksinangil) — Config overlay fix, GLM-5.2 default
- [@yasaricli](https://github.com/yasaricli) — `clother bench` command, GLM-5.1 support
- [@jeliseocd](https://github.com/jeliseocd) — Config overlay collision report and diagnosis

## Star History

[![Star History Chart](https://api.star-history.com/svg?repos=jolehuit/clother&type=Date)](https://www.star-history.com/#jolehuit/clother&Date)

## License

MIT © [jolehuit](https://github.com/jolehuit)


### Live session controls and budgets (v3.6)

Free-model sessions automatically receive a Clother statusline in a temporary
Claude settings overlay. It shows the actual provider/model, Free/Paid, context,
known quota, local budget reservations and the latest switch reason. Your original
settings are preserved; `CLOTHER_STATUSLINE=0` keeps your usual statusline instead.
Unknown quota is displayed as `?`, particularly for Kilo's shared IP allowance.

- `/clother:next`: try the next eligible route at the next request boundary.
- `/clother:pin`: keep this backend and stop automatic switches.
- `/clother:free`: block new paid requests and select an eligible free route.
- `/clother:auto`: resume the configured automatic fallback policy.

These fixed controls execute before the model request, so they do not depend on a
model deciding to run a tool. In-flight responses finish; partial output is never
replayed. After a paid fallback, free recovery checks run at request boundaries,
at most every five minutes. An eligible free model must pass a small two-turn
probe before the session returns to it. Probes consume free requests; known quota
blocks still apply. No background polling is performed while idle.

Paid fallbacks default to **$1 per UTC day and $1 per launched session**:

```sh
clother config budget 1 1
clother config budget 0 0   # blocks paid fallback
clother usage --json
clother doctor
clother doctor kilo stealth/space-bunny-alpha probe --json
```

Budget changes apply to new sessions. Use `/clother:free` to stop new paid requests
in a running one. The local ledger reserves the model's full context plus bounded
output at maximum rates before sending a paid request, including across concurrent
Clother processes. Complete token usage settles conservatively; unknown completion
retains the reservation. Billed cost, when returned, is tracked separately from
estimates. The limit covers this installation's automatic fallbacks, not other
applications or direct paid launches. Changing provider prices or incorrect custom
rates can differ from billing; provider-side spend caps remain authoritative.

Direct `deepseek-flash` uses current official DeepSeek peak/cache-miss pricing,
refreshed hourly. Unknown/expired prices block paid inference. Other configured
keyed endpoints require explicit rates in `config.json`, for example:

```json
{"budget":{"daily_usd":1,"session_usd":1,"prices":{"my-provider/my-model":{"input_usd_per_million":0.3,"output_usd_per_million":1.2,"expires_at":"2026-10-01T00:00:00Z","source":"your endpoint's verified price schedule"}}}}
```

Use prices for the actual endpoint, including its maximum applicable charges.
An unreadable ledger or abandoned `budget/v1/lock` directory blocks paid requests;
`doctor` reports it. Never delete a lock while Clother processes may still use it.
The ledger contains metadata only. Unresolved reservations are retained after a
crash, rather than silently released.

Selection combines exact-version benchmark evidence with local 24-hour response
success, mean latency and generated tool-call observations. Three consecutive
non-quota failures put a model on a ten-minute cooldown. These operational metrics
are not a quality benchmark or proof that a generated tool executed correctly.
`doctor ... probe` verifies an actual harmless tool round trip and consumes two
small free requests; it never executes model-generated commands. Without `probe`,
diagnostics do not perform inference. JSON output omits keys, prompts and raw
provider responses.


`clother doctor` groups system checks, each provider, paid fallback budgets and
next steps into separate sections. Terminal output uses green success badges,
yellow setup/limit notices and red failures. Budget bars show local estimates and
reservations; quota bars appear only when the provider returns a measured counter.
Context/output values are model limits, not consumed tokens. Missing Kilo quota
remains explicitly unknown; anonymous access is shown as optional-key access.
Colors are disabled for redirected output, `NO_COLOR` and `--plain`; `--json`
retains the machine-readable diagnostic format. No inference is performed unless
`probe` is requested.
