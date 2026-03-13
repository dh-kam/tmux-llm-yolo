# tmux-llm-yolo

`tmux-llm-yolo` is a watcher for `tmux` sessions that run CLI coding assistants such as Codex, GLM/Claude-style CLIs, Copilot, and Gemini.

It watches the assistant session, decides whether the assistant is still working or is now waiting for user input, and then either:

- keeps waiting
- selects a menu option
- submits pending input
- injects the next continuation prompt

The main goal is to reduce manual supervision and to avoid unnecessary LLM calls while supervising long-running coding sessions.

## Why this exists

Many CLI assistants support aggressive or "yolo" modes, but they still stop often for:

- confirmation prompts
- numbered menus
- free-text follow-up requests
- completion summaries that need "continue"

This project runs outside the assistant, watches the target `tmux` pane, and reacts only when the screen is confidently stable.

## Core behavior

The watcher uses two capture modes for the same pane:

- ANSI-inclusive capture
  - used for screen stability checks and active prompt detection
- ANSI-stripped capture
  - used for reading the latest meaningful output block above the prompt

Waiting detection is stability-first:

1. capture ANSI/plain at the base interval
2. compare the current ANSI capture to the previous ANSI capture
3. only if 5 ANSI captures match across at least 20 seconds total (`0s`, `5s`, `10s`, `15s`, `20s`) does the watcher treat the session as waiting
4. only then does it interpret the screen and decide the next action

Deterministic rules are preferred first.
LLM usage is fallback-only when the screen is confidently waiting but still not classified with confidence.

## Waiting-state classes

When a session is confidently stable, the watcher classifies it into one of these:

- `continue_after_completion`
- `free_text_request`
- `numbered_multiple_choice`
- `cursor_based_choice`
- `completed_no_further_action`
- `unknown_waiting`

Typical actions:

- inject a continuation prompt
- send a numeric answer
- send navigation keys plus submit
- stop without sending anything

## Provider-specific handling

The watcher currently contains provider-aware heuristics for:

- Codex
- GLM / Claude-style terminal UIs
- GitHub Copilot CLI-style UI
- Gemini CLI-style UI

Examples of provider-specific logic:

- active prompt line detection using ANSI style
- placeholder versus typed text detection
- special submit-key handling
- slash-command cleanup for Copilot-style prompts

## Runtime model

The runtime is queue-driven rather than a loop with ad hoc sleeps.

High-level states:

- `monitoring`
- `suspect_waiting_stage_1`
- `suspect_waiting_stage_2`
- `suspect_waiting_stage_3`
- `confident_waiting`
- `interpreting`
- `acting`
- `completed`
- `stopped`

The TUI is built with Bubble Tea and Lip Gloss and shows:

- current state
- current task
- next queued task
- countdown while sleeping
- watch deadline
- primary/fallback LLM status

## Requirements

- Go 1.22+
- `tmux`
- one or more target assistant sessions already running in `tmux`

Optional:

- configured CLI LLM providers for semantic fallback

## Quick start

Run once against a target session:

```bash
go run . watch --once -t tmp-codex --llm codex --fallback-llm glm
```

Run continuous watch:

```bash
go run . watch -t tmp-glm --llm glm --fallback-llm codex
```

Show version:

```bash
go run . --version
```

Install local git hooks:

```bash
tools/dev-init.sh
```

## Build and test

Run tests:

```bash
make test
```

Build all Linux targets:

```bash
make linux
```

Build a specific target:

```bash
make linux-amd64-debug
make linux-amd64-release
make linux-arm64-debug
make linux-arm64-release
```

Create release archives:

```bash
make release-artifacts
```

Artifacts are produced under:

- `build/`
- `dist/`

## Versioning

Source version lives in:

- [internal/buildinfo/version.go](internal/buildinfo/version.go)

Version format:

- `vMAJOR.MINOR.REV-YYYYMM.SEQ`

Examples:

- `v0.9.0-202603.1`
- `v1.2.0-202604.3`

Update the version explicitly:

```bash
tools/bump_up.sh --to v0.9.1-202603.2
```

Bump only the `YYYYMM.SEQ` suffix:

```bash
tools/bump_up.sh --seq
```

Bump semantic parts:

```bash
tools/bump_up.sh --major
tools/bump_up.sh --minor
tools/bump_up.sh --rev
```

Rules:

- `tools/dev-init.sh` installs the repository git hooks via `core.hooksPath=.githooks`
- the installed `pre-commit` hook advances only `YYYYMM.SEQ` on each commit
- if `YYYYMM` matches the previous committed version, `SEQ` increases by `1`
- if `YYYYMM` changes, `SEQ` resets to `1`
- `--major` increments major and resets minor/rev to `0`
- `--minor` increments minor and resets rev to `0`
- `--rev` increments rev
- `YYYYMM` uses the current UTC month
- `SEQ` increments if the current version is already in the same UTC month, otherwise resets to `1`

## GitHub Actions

### Test workflow

On `push` and `pull_request`:

- runs `make test`

Workflow file:

- [.github/workflows/test.yml](.github/workflows/test.yml)

### Build workflow

On `push`, `pull_request`, and manual dispatch:

- runs `make linux-amd64-release`
- runs `make linux-arm64-release`
- uploads the exact build outputs as Actions artifacts
- artifact `linux-amd64-release` contains `build/linux-amd64/release/tmux-llm-yolo`
- artifact `linux-arm64-release` contains `build/linux-arm64/release/tmux-llm-yolo`

Workflow file:

- [.github/workflows/build.yml](.github/workflows/build.yml)

### Release workflow

Manual workflow dispatch:

- optional `version` input
- if provided, updates the source version before release
- if omitted, uses the version already in source
- runs tests
- builds release artifacts
- creates a git tag
- publishes a GitHub release

Workflow file:

- [.github/workflows/release.yml](.github/workflows/release.yml)

## Notes

- `go run . --version` shows the source version.
- built binaries created through `make` also receive commit and build-date via linker flags.
- Linux artifacts cannot be executed directly on non-Linux hosts.
