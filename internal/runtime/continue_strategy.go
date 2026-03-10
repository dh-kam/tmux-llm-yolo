package runtime

import "strings"

var defaultContinuePrompts = []string{
	"계속 진행하되 중간에 막히는 지점이 있으면 스스로 가설을 세우고 검증까지 이어서 진행해보자.",
	"다음 작업을 이어서 진행하고, 변경 이유와 영향 범위를 짧게 정리하면서 계속 진행해보자.",
	"남은 작업을 우선순위대로 하나씩 처리하고, 끝난 항목과 남은 항목을 구분해서 계속 진행해보자.",
	"구조를 보면서 개선 포인트를 바로 적용 가능한 것부터 하나씩 처리해보자.",
	"테스트 가능한 단위부터 정리하고, 작은 검증을 끼워 넣으면서 계속 진행해보자.",
}

var auditContinuePrompts = []string{
	"지금까지의 진행률을 점검하고 미진한 부분, 누락된 작업, 남은 리스크를 리스트업한 뒤 우선순위가 높은 것부터 계속 진행해보자.",
	"나는 clean architecture, single responsibility principle, interface-oriented programming을 선호한다. 현재 코드에서 어긋나는 부분을 찾아 개선 목록을 만들고 하나씩 진행해보자.",
	"s/w architect 관점에서 현재 구조를 분석하고 결합도, 책임 분리, 확장성, 테스트 용이성 측면의 개선 포인트를 정리한 뒤 가장 효과 큰 것부터 적용해보자.",
	"지금까지 변경된 구조를 다시 검토해서 경계가 불명확한 모듈, 과도한 책임을 가진 파일, 인터페이스 추상화가 필요한 지점을 찾아 순서대로 개선해보자.",
	"중간 점검을 하자. 이미 끝난 것과 아직 불완전한 것을 구분하고, 구현은 되었지만 검증이 약한 부분을 찾아 보강하면서 계속 진행해보자.",
}

type continueStrategy struct {
	basePrompts  []string
	auditPrompts []string
	baseFallback string
}

func newContinueStrategy(baseFallback string) continueStrategy {
	baseFallback = strings.TrimSpace(baseFallback)
	if baseFallback == "" {
		baseFallback = "계속 진행하되 완료까지 이어서 처리해보자."
	}
	return continueStrategy{
		basePrompts:  defaultContinuePrompts,
		auditPrompts: auditContinuePrompts,
		baseFallback: baseFallback,
	}
}

func (s continueStrategy) messageFor(continueSentCount int) string {
	if continueSentCount <= 0 {
		return s.baseFallback
	}
	if continueSentCount%20 == 0 && len(s.auditPrompts) > 0 {
		idx := ((continueSentCount / 20) - 1) % len(s.auditPrompts)
		return s.auditPrompts[idx]
	}
	if len(s.basePrompts) == 0 {
		return s.baseFallback
	}
	idx := (continueSentCount - 1) % len(s.basePrompts)
	return s.basePrompts[idx]
}

func (s continueStrategy) nextAuditIn(continueSentCount int) int {
	if continueSentCount < 0 {
		continueSentCount = 0
	}
	remainder := continueSentCount % 20
	if remainder == 0 {
		return 20
	}
	return 20 - remainder
}
