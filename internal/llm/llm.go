package llm

import (
	"context"
	"io"
)

type Usage struct {
	HasKnownLimit bool
	Remaining     int
	Source        string
}

type Capture struct {
	Text string
}

func NewCapture(text string) Capture {
	return Capture{Text: text}
}

func NewCaptureFromBytes(data []byte) Capture {
	return Capture{Text: string(data)}
}

func NewCaptureFromReader(reader io.Reader) (Capture, error) {
	data, err := io.ReadAll(reader)
	if err != nil {
		return Capture{}, err
	}
	return Capture{Text: string(data)}, nil
}

type Provider interface {
	Name() string
	Binary() string
	ValidateBinary() (string, error)
	CheckUsage(context.Context) (Usage, error)
	RunPrompt(context.Context, string) (string, error)
	IsProgressingCapture(Capture) (bool, string)
}
