# Refactor Plan

## Goals

- Reduce `watcher.go` to orchestration and state-machine control.
- Separate screen understanding, interaction planning, and action execution.
- Support multiple assistant UIs through provider-specific adapters instead of scattered conditionals.
- Make strategy selection explicit through swappable policies.
- Move toward interface-driven, testable, small-unit components.

## Design Principles

- Prefer capability-based abstractions over provider-first package splits.
- Keep provider variation behind narrow interfaces.
- Let deterministic experts run before any LLM fallback.
- Separate "what input is required" from "how to send that input".
- Make policies influence decision thresholds, continuation guidance, and completion pressure.
- Keep the migration incremental so the current watcher behavior stays working.

## Target Architecture

### Domain

- `ScreenSnapshot`
- `PromptState`
- `InteractionRequirement`
- `ActionPlan`
- `Policy`

These are provider-agnostic types used across analyzer, satisfier, executor, and runtime.

### Analysis Layer

- `PromptLocator`
- `OutputExtractor`
- `InteractivePromptDetector`
- `WaitingClassifier`
- `LowActivityDetector`
- `DetectionExpertRegistry`

This layer answers: "What is happening on the screen?"

### Resolution Layer

- `RequirementSatisfier`
- `FreeTextSatisfier`
- `PlannedItemsSatisfier`
- `NumberedChoiceSatisfier`
- `CursorChoiceSatisfier`

This layer answers: "What user input would satisfy the current requirement?"

### Execution Layer

- `ActionExecutor`
- `CodexExecutor`
- `GLMExecutor`
- `GeminiExecutor`
- `CopilotExecutor`

This layer answers: "How should this action be performed in this UI?"

### Policy Layer

- `poc-completion`
- `aggressive-architecture`
- `parity-porting`
- `creative-exploration`

Policies should affect:

- continue prompt rotation
- audit prompt cadence
- ambiguity handling
- done/completed strictness
- validation pressure
- architecture/performance emphasis
- LLM fallback prompt shaping

### Runtime Layer

`Runner` should eventually depend on interfaces and compose:

- capture service
- analyzer pipeline
- requirement satisfier registry
- action executor registry
- selected policy
- state-machine task queue

## Proposed Interfaces

### Interaction requirement

```go
type InteractionKind string

const (
    InteractionFreeText      InteractionKind = "free_text"
    InteractionPlannedText   InteractionKind = "planned_text"
    InteractionNumberedChoice InteractionKind = "numbered_choice"
    InteractionCursorChoice   InteractionKind = "cursor_choice"
)

type InteractionRequirement struct {
    Kind           InteractionKind
    Context        string
    Prompt         PromptState
    Options        []Option
    SuggestedValue string
}
```

### Satisfier

```go
type RequirementSatisfier interface {
    Supports(InteractionRequirement) bool
    BuildPlan(context.Context, InteractionRequirement, policy.Policy) (ActionPlan, error)
}
```

### Executor

```go
type ActionExecutor interface {
    Provider() string
    Execute(context.Context, ActionPlan) error
}
```

### Policy

```go
type Policy interface {
    Name() string
    Description() string
    Continuation() ContinuationSpec
    Decision() DecisionSpec
    Validation() ValidationSpec
    Quality() QualitySpec
}
```

## Migration Strategy

### Phase 1: Policy foundation

- introduce `internal/policy`
- define policy interface, specs, registry, default policies
- route continue strategy through selected policy without changing runtime behavior

### Phase 2: Action planning foundation

- introduce domain types for `InteractionRequirement` and `ActionPlan`
- separate planning from execution
- keep current runtime methods as adapters during migration

### Phase 3: Executor split

- isolate actual key sending logic into provider-aware executors
- move submit/clear/cursor behavior out of `watcher.go`

### Phase 4: Analyzer split

- break prompt detection into small experts
- add expert registry and pipeline ordering
- keep deterministic-first classification

### Phase 5: Runtime simplification

- shrink `Runner` to queue/state orchestration
- remove direct provider-specific branches from runtime

## Task List

- [x] Write and agree on the target refactor plan in `plan.md`.
- [x] Introduce `internal/policy` with policy interface, specs, registry, and built-in policies.
- [x] Refactor `continueStrategy` to consume policy continuation settings instead of local hardcoded prompt lists.
- [x] Add runtime-level policy selection plumbing with a default policy and safe fallback behavior.
- [x] Introduce domain types for `InteractionRequirement`, `Option`, and `ActionPlan`.
- [x] Extract action execution interfaces from `watcher.go` and wrap current send helpers behind executors.
- [x] Introduce provider-aware executors for `glm`, `codex`, `gemini`, and `copilot`.
- [x] Split prompt detection into detector experts and add a registry/pipeline.
- [ ] Split waiting interpretation into requirement-building logic and satisfier registry.
- [ ] Route LLM fallback prompt shaping through the active policy.
- [ ] Reduce `Runner` responsibilities so it mainly coordinates queue/state transitions.
- [ ] Expand unit tests around policy selection, expert ordering, satisfier selection, and executor behavior.

## Current Step

The next implementation step is to split waiting interpretation into requirement-building logic and a satisfier registry so deterministic planning becomes replaceable by policy-aware modules.
