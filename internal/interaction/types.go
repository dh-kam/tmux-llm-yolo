package interaction

type Kind string

const (
	KindUnknown        Kind = "unknown"
	KindFreeText       Kind = "free_text"
	KindPlannedText    Kind = "planned_text"
	KindNumberedChoice Kind = "numbered_choice"
	KindCursorChoice   Kind = "cursor_choice"
	KindContinue       Kind = "continue"
)

type Option struct {
	Value      string
	Label      string
	Selected   bool
	Persistent bool
}

type Requirement struct {
	Kind           Kind
	Context        string
	Prompt         string
	Options        []Option
	SuggestedValue string
	Reason         string
}

type ActionKind string

const (
	ActionUnknown      ActionKind = "unknown"
	ActionContinue     ActionKind = "continue"
	ActionInputText    ActionKind = "input_text"
	ActionChoice       ActionKind = "choice"
	ActionCursorChoice ActionKind = "cursor_choice"
)

type ActionStep struct {
	Kind   ActionKind
	Value  string
	Reason string
}

type ActionPlan struct {
	Requirement Requirement
	Steps       []ActionStep
	Reason      string
}
