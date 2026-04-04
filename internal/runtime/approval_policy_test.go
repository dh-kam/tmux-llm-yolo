package runtime

import (
	"testing"
)

func TestApprovalPolicy_FirstFiveAreOneTime(t *testing.T) {
	p := NewApprovalPolicy(5)
	ctx := "go test ./internal/cache -count=1"

	for i := 1; i <= 5; i++ {
		choice := p.Decide(ctx)
		if choice.Option != 1 {
			t.Fatalf("approval %d: expected option 1 (allow once), got %d", i, choice.Option)
		}
	}
}

func TestApprovalPolicy_SixthIsSessionWide(t *testing.T) {
	p := NewApprovalPolicy(5)
	ctx := "go test ./internal/cache -count=1"

	for i := 0; i < 5; i++ {
		p.Decide(ctx)
	}

	choice := p.Decide(ctx)
	if choice.Option != 2 {
		t.Fatalf("approval 6: expected option 2 (session-wide), got %d", choice.Option)
	}
}

func TestApprovalPolicy_DifferentCommandsTrackSeparately(t *testing.T) {
	p := NewApprovalPolicy(2)

	// "go test" gets 2 one-time approvals
	p.Decide("go test ./pkg1")
	p.Decide("go test ./pkg2")

	// 3rd "go test" should be session-wide
	choice := p.Decide("go test ./pkg3")
	if choice.Option != 2 {
		t.Fatalf("expected session-wide for 3rd go test, got option %d", choice.Option)
	}

	// "gofmt" is a different command, should still be one-time
	choice = p.Decide("gofmt -w main.go")
	if choice.Option != 1 {
		t.Fatalf("expected one-time for first gofmt, got option %d", choice.Option)
	}
}

func TestApprovalPolicy_SessionWideStaysPermanent(t *testing.T) {
	p := NewApprovalPolicy(1)
	ctx := "npm run build"

	p.Decide(ctx) // 1st = one-time
	for i := 0; i < 10; i++ {
		choice := p.Decide(ctx)
		if choice.Option != 2 {
			t.Fatalf("iteration %d: expected session-wide after threshold, got %d", i, choice.Option)
		}
	}
}

func TestExtractApprovalKey(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"go test ./internal/cache -count=1", "go test"},
		{"gofmt -w internal/cache/cache.go", "gofmt"},
		{"$ go test -v .", "go test"},
		{"Shell go test -v .", "go test"},
		{"npm run build", "npm run"},
		{"cargo test --release", "cargo test"},
		{"python -m pytest", "python -m"},
		{"make linux", "make linux"},
		{"Reason: 최종 수정 후 캐시 패키지 테스트", "최종"},
		{"", "_empty"},
	}

	for _, tc := range tests {
		got := extractApprovalKey(tc.input)
		if got != tc.want {
			t.Errorf("extractApprovalKey(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestApprovalPolicy_Count(t *testing.T) {
	p := NewApprovalPolicy(5)
	p.Decide("go test ./a")
	p.Decide("go test ./b")
	p.Decide("gofmt -w x.go")

	if c := p.Count("go test ./c"); c != 2 {
		t.Fatalf("expected count 2 for 'go test', got %d", c)
	}
	if c := p.Count("gofmt -w y.go"); c != 1 {
		t.Fatalf("expected count 1 for 'gofmt', got %d", c)
	}
	if tc := p.TotalCount(); tc != 3 {
		t.Fatalf("expected total count 3, got %d", tc)
	}
}
