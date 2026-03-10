# PRD.md

## Product

`r8-watcher` is an autonomous watcher for `tmux` sessions running CLI coding assistants.
Its job is to detect when the assistant is actively working versus waiting for the human, and to take the next appropriate interaction step with minimal LLM usage.

## Problem

The current implementation relies too much on periodic capture plus prompt/LLM classification.
That causes unnecessary LLM calls and weakens reliability around waiting-state detection.

## New Product Direction

The product will move to a stability-first runtime:

1. Detect unchanged ANSI screens over time.
2. Escalate through timed suspicion stages.
3. Confirm likely waiting state before semantic interpretation.
4. Interpret the final visible output around the prompt area.
5. Act using deterministic logic first and LLM fallback second.
6. Present the runtime as a visible task queue with countdown UI.

## Primary Goals

1. Reduce LLM usage materially.
2. Improve confidence in “assistant is waiting for me” detection.
3. Support multiple CLI assistants without hardcoding the entire flow per provider.
4. Make runtime behavior observable through a countdown/task UI.
5. Make the watcher stoppable and deadline-aware.

## Non-Goals

1. Building a full terminal emulator inside the watcher.
2. Solving every provider-specific UI edge case in the first pass.
3. Replacing tmux as the execution environment.

## Functional Requirements

### FR-1 Dual capture

On each base monitoring cycle, the system must collect:

- ANSI-inclusive capture
- ANSI-stripped capture

### FR-2 Waiting suspicion ladder

If the current ANSI-inclusive capture matches the previous base-cycle ANSI-inclusive capture:

- mark suspicion stage 1
- wait 5 seconds
- recapture ANSI-inclusive output

If it still matches:

- mark suspicion stage 2
- wait 5 seconds
- recapture ANSI-inclusive output

If it still matches:

- mark suspicion stage 3
- wait 5 seconds
- recapture ANSI-inclusive output

If it still matches:

- mark confident waiting state

The watcher should only treat the session as confidently waiting after 5 identical ANSI-inclusive captures across at least 20 seconds total: `0s`, `5s`, `10s`, `15s`, `20s`.

### FR-3 Prompt detection

The system must detect likely prompt/input location from ANSI-inclusive capture using generic plus provider-specific heuristics.

### FR-4 Last output block extraction

The system must use ANSI-stripped capture to extract the final meaningful output block above the prompt.

### FR-5 Waiting-state classification

The system must classify confident waiting screens into:

- continue after completion
- free text request
- numbered choice
- cursor-based choice
- completed with nothing more to do
- unknown waiting

### FR-6 Appropriate action

The system must support at least:

- sending free text
- sending numeric choice
- sending navigation keys for cursor-based menus
- stopping when the task appears fully done

For free-text continuation prompts, the system should rotate among multiple continuation strategies instead of repeating a single message forever.
Every 20th continuation should use an audit-style prompt that explicitly asks for progress review, missing work discovery, architectural cleanup, or design-principle alignment.

### FR-7 Queue-driven runtime

The system must operate through explicit tasks rather than a loop with inline sleeps.

### FR-8 TUI visibility

The system must display:

- current task
- current state
- sleep countdown in seconds
- reason for waiting
- next queued action
- remaining overall watch time

Implementation target: Bubble Tea plus Lip Gloss.

### FR-9 Deadline enforcement

The system must stop when the configured total watch duration is exceeded.

### FR-10 LLM fallback provider

The system must support a primary LLM and an optional fallback LLM.
If semantic fallback requires a model and the primary provider is unavailable, exhausted, or fails the request, the watcher should automatically try the fallback provider.

### FR-10 Once mode

When `--once` is used:

- the system must inspect the current screen immediately
- it must use LLM-assisted interpretation of the current state
- it must perform at most one resulting tmux action
- it must exit immediately after that action, or exit without action if the screen is clearly complete/no-op

## Suggested Architecture

### Core packages

- `internal/runtime`
  - queue, task executor, deadline handling
- `internal/state`
  - watcher state machine
- `internal/capture`
  - dual capture acquisition, normalization, comparisons
- `internal/prompt`
  - prompt detection from ANSI-inclusive data
- `internal/interpret`
  - output block extraction and waiting classification
- `internal/tui`
  - Bubble Tea/Lip Gloss rendering

### Existing packages to retain

- `internal/tmux`
- `internal/llm`

## Milestones

### Milestone 1

- Introduce queue and state machine skeleton.
- Add dual-capture data model.
- Add `SleepTask` and `CheckDeadlineTask`.
- Replace ad hoc `time.Sleep` logic.

### Milestone 2

- Add ANSI stability comparison ladder: 5s -> 5s -> 5s -> 5s.
- Preserve previous captures in runtime state.
- Add logging/state artifact support for stability transitions.

### Milestone 3

- Implement prompt detection on ANSI-inclusive captures.
- Implement output block extraction using ANSI-stripped captures.
- Add provider-specific heuristics behind a generic interface.

### Milestone 4

- Add deterministic waiting-state classification.
- Support numbered and cursor-based selection actions.
- Restrict LLM usage to unresolved waiting states.

### Milestone 5

- Add Bubble Tea/Lip Gloss runtime UI.
- Display active countdown and next queued action.

### Milestone 6

- Remove obsolete early-LLM pathways.
- Expand fixtures and integration tests for each provider style.

## Acceptance Criteria

1. The watcher does not call an LLM during ordinary “screen still changing” periods.
2. The watcher only enters semantic interpretation after the 5s/5s/5s/5s unchanged ANSI ladder completes, spanning at least 20 seconds total.
3. The watcher can distinguish text-input prompts from numbered menus and cursor-based menus in at least the common provider cases.
4. The runtime shows a live countdown and queued next step while waiting.
5. The watcher respects the overall runtime limit and exits cleanly.

## Notes For Future Changes

- Any change that reintroduces frequent unconditional LLM calls violates this PRD.
- Prompt detection should be generalized first, provider-specialized second.
- If deterministic interpretation is good enough, prefer it over model inference.
