# AGENTS.md

## Purpose

This project watches `tmux` sessions that run CLI coding assistants such as Codex, Claude/GLM, Copilot, and Gemini.
The watcher exists to reduce manual supervision by detecting when the assistant is no longer actively progressing and is instead waiting for user input, then choosing or injecting the next input automatically.

## Working Rules

1. Do not use the old policy of immediately asking an LLM on every capture cycle.
2. Prefer local, deterministic detection before any LLM call.
3. Treat ANSI-inclusive captures and plain-text captures as separate data sources with different purposes.
4. Use ANSI-inclusive capture to detect screen stability and prompt/input box location.
5. Use ANSI-stripped capture to read the semantic content around the prompt and extract the latest output block.
6. LLM usage is a fallback for semantic interpretation after the watcher is confident the session is waiting for input.
7. The watcher must be driven by an explicit state machine and a task queue, not ad hoc sleeps spread through the loop.
8. Every queued task must contain a human-readable description of why it exists and what will happen next.
9. While sleeping or waiting, the terminal UI must show a countdown and the next queued action using Bubble Tea and Lip Gloss.
10. The total watch duration limit must be enforced continuously by queue/state logic, not only by outer loop structure.
11. Continue prompts must not be a single static sentence forever; they should rotate across multiple architecture/improvement-oriented prompts.
12. Every 20th continue injection should switch to an audit-style prompt that asks for progress review, missing work, architectural cleanup, or design-quality improvement.
13. LLM fallback should support a primary provider and an optional backup provider; if the primary is unavailable or fails during semantic fallback, the backup should be tried next.

## Capture Policy

### Base cycle

- Default polling cadence is 5 seconds.
- On each base cycle, capture the target session twice:
  - ANSI-inclusive capture
  - ANSI-stripped capture

### Stability-based waiting suspicion

- Compare the current ANSI-inclusive capture with the previous base-cycle ANSI-inclusive capture.
- If different:
  - Clear waiting suspicion state.
  - Return to the normal 5-second cadence.
- If identical:
  - Enter first waiting-suspicion state.
  - Schedule a recheck in 5 seconds using ANSI-inclusive capture.

### Escalation

- If the first 5-second ANSI-inclusive recheck still matches:
  - Enter second waiting-suspicion state.
  - Schedule another recheck in 5 seconds using ANSI-inclusive capture.
- If the second 5-second ANSI-inclusive recheck still matches:
  - Enter third waiting-suspicion state.
  - Schedule another recheck in 5 seconds using ANSI-inclusive capture.
- If the third 5-second ANSI-inclusive recheck still matches:
  - Enter confident waiting state.
  - Begin prompt detection and semantic interpretation.
- The watcher must only treat the session as confidently waiting after 5 identical ANSI-inclusive captures across at least 20 seconds total: `0s`, `5s`, `10s`, `15s`, `20s`.

## Prompt Detection Policy

### Prompt location

- Prompt location detection must use ANSI-inclusive capture.
- Detection should be provider-agnostic where possible, but allow provider-specific rules when necessary.
- The implementation must support patterns such as:
  - highlighted input box
  - reversed/background-colored line
  - shell-like prompt markers such as `>`
  - numbered menu cursor area
  - cursor-select UI rather than raw typed numeric input

### Output block extraction

- Once prompt location is identified, use the ANSI-stripped capture to inspect lines above it.
- Extract the last meaningful output block, which may be:
  - a short final status line
  - a paragraph
  - a numbered menu
  - a cursor-based selection menu
  - a completion summary
  - a “nothing more to do” summary

## Interpretation Policy

When the watcher reaches confident waiting state, classify the screen into one of these categories before deciding input:

1. `continue_after_completion`
2. `free_text_request`
3. `numbered_multiple_choice`
4. `cursor_based_choice`
5. `completed_no_further_action`
6. `unknown_waiting`

### Action expectations

- `continue_after_completion`
  - inject the configured continue prompt or equivalent free-text continuation.
- `free_text_request`
  - generate or select the required text input.
- `numbered_multiple_choice`
  - input the chosen number and submit.
- `cursor_based_choice`
  - send navigation keys plus submit, not raw number text.
- `completed_no_further_action`
  - do not loop forever injecting text; mark the session as effectively done.
- `unknown_waiting`
  - only then consider LLM-based semantic classification.

## LLM Usage Policy

Use an LLM only when all of the following are true:

1. ANSI stability has passed all waiting-suspicion stages.
2. Prompt location detection has succeeded or the screen still strongly appears idle/waiting.
3. Deterministic rules could not confidently classify the waiting state.

Do not use an LLM for:

- simple progress detection
- simple unchanged-screen detection
- session existence checks
- cases where deterministic menu parsing already identifies the needed action

## Runtime Model

### State machine

Expected high-level states:

- `monitoring`
- `suspect_waiting_stage_1`
- `suspect_waiting_stage_2`
- `suspect_waiting_stage_3`
- `confident_waiting`
- `interpreting`
- `acting`
- `completed`
- `stopped`

### Task queue

The runtime should execute through a queue of explicit tasks.
Examples:

- `CaptureTask`
- `CompareCaptureTask`
- `DetectPromptTask`
- `ExtractOutputBlockTask`
- `InterpretWaitingStateTask`
- `InjectInputTask`
- `SleepTask`
- `CheckDeadlineTask`
- `StopTask`

### SleepTask requirements

Each `SleepTask` must store:

- reason text
- sleep duration
- creation time
- deadline/expected wake time
- description of the next queued task

The Bubble Tea UI should render:

- current state
- current task
- remaining sleep time in 1-second resolution
- next task description
- overall watch deadline remaining

## Once Mode Policy

- `--once` is a special execution mode.
- In `--once`, the watcher should inspect the current session immediately rather than waiting through the full 5s/5s/5s/5s ladder.
- `--once` should use LLM interpretation for the current waiting screen and then execute the corresponding single action.
- After the resulting tmux key/input task is sent once, the process must exit.
- If the screen is clearly completed with no further action, the process may exit without sending input.

## Deadline Policy

- The watcher has a total allowed runtime, for example 10 hours.
- Deadline enforcement must be modeled explicitly, preferably via `CheckDeadlineTask`.
- Before or after significant waits/actions, the queue must verify whether the watch deadline has been exceeded.
- If exceeded, stop cleanly and report why.

## Migration Policy

When changing code:

1. Prefer introducing the state machine and queue first.
2. Move capture comparison logic ahead of LLM interpretation.
3. Isolate provider-specific prompt detection into dedicated helpers.
4. Keep existing tmux and LLM abstractions if they still fit.
5. Remove or demote any logic that calls an LLM too early in the flow.
