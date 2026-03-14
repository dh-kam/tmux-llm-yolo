package prompt

type analysisContext struct {
	providerHint string
	ansiCapture  string
	plainCapture string
	locale       string
	paneWidth    int
	ansiLines    []string
	plainLines   []string
	heuristics   providerHeuristics
}

type analysisExpert interface {
	Name() string
	Apply(*analysisContext, *Analysis)
}

type providerExpert struct{}

func (providerExpert) Name() string { return "provider" }

func (providerExpert) Apply(ctx *analysisContext, analysis *Analysis) {
	provider := detectProviderFromCaptures(ctx.ansiCapture, ctx.plainCapture)
	if provider == "" {
		provider = normalizeProviderHint(ctx.providerHint)
	}
	analysis.Provider = provider
	ctx.heuristics = heuristicsFor(provider)
}

type promptLineExpert struct{}

func (promptLineExpert) Name() string { return "prompt_line" }

func (promptLineExpert) Apply(ctx *analysisContext, analysis *Analysis) {
	promptLine := ctx.heuristics.detectPromptLine(ctx.ansiLines, ctx.plainLines)
	if promptLine < 0 {
		promptLine = detectLastNonEmptyLine(ctx.plainLines)
	}
	analysis.PromptDetected = promptLine >= 0
	analysis.PromptLine = promptLine
}

type promptStateExpert struct{}

func (promptStateExpert) Name() string { return "prompt_state" }

func (promptStateExpert) Apply(ctx *analysisContext, analysis *Analysis) {
	if analysis.PromptLine >= 0 && analysis.PromptLine < len(ctx.plainLines) {
		analysis.PromptText = collectPromptText(analysis.PromptLine, ctx.plainLines, ctx.paneWidth)
	}
	analysis.PromptActive = ctx.heuristics.isActivePromptLine(analysis.PromptLine, ctx.ansiLines, ctx.plainLines)
	analysis.PromptPlaceholder = ctx.heuristics.isPlaceholderPromptLine(analysis.PromptLine, ctx.ansiLines, ctx.plainLines)
}

type assistantUIExpert struct{}

func (assistantUIExpert) Name() string { return "assistant_ui" }

func (assistantUIExpert) Apply(ctx *analysisContext, analysis *Analysis) {
	analysis.AssistantUI = ctx.heuristics.hasAssistantUI(ctx.ansiCapture, ctx.plainCapture)
	if !analysis.AssistantUI &&
		analysis.Provider != "" &&
		analysis.Provider == normalizeProviderHint(ctx.providerHint) &&
		analysis.PromptDetected {
		analysis.AssistantUI = true
	}
}

type outputBlockExpert struct{}

func (outputBlockExpert) Name() string { return "output_block" }

func (outputBlockExpert) Apply(ctx *analysisContext, analysis *Analysis) {
	analysis.OutputBlock = extractOutputBlock(ctx.plainLines, analysis.PromptLine)
}

type processingExpert struct{}

func (processingExpert) Name() string { return "processing" }

func (processingExpert) Apply(ctx *analysisContext, analysis *Analysis) {
	analysis.Processing = isProcessing(*analysis, ctx.plainLines)
}

type classificationExpert struct{}

func (classificationExpert) Name() string { return "classification" }

func (classificationExpert) Apply(ctx *analysisContext, analysis *Analysis) {
	analysis.Classification, analysis.RecommendedChoice, analysis.Reason = classifyForLocale(*analysis, ctx.locale)
}

type interactivePromptExpert struct{}

func (interactivePromptExpert) Name() string { return "interactive_prompt" }

func (interactivePromptExpert) Apply(ctx *analysisContext, analysis *Analysis) {
	analysis.InteractivePrompt = hasInteractivePrompt(*analysis)
}

func defaultAnalysisExperts() []analysisExpert {
	return []analysisExpert{
		providerExpert{},
		promptLineExpert{},
		promptStateExpert{},
		assistantUIExpert{},
		outputBlockExpert{},
		processingExpert{},
		classificationExpert{},
		interactivePromptExpert{},
	}
}
