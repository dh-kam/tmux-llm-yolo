# User Prompt Preference Analysis

- Source: `/workspace/dev-pref/ai-history/output/user-prompts-20260404.json`
- Generated: `2026-04-04T19:20:58.381971+09:00`
- Selected prompts: `1404`
- Archetypes: `34`

## 추출 기준
- 남긴 것: 프레임워크 선택, 빌드/배포 구조, 테스트 철학, 아키텍처 원칙, 문서/컨벤션, 그리고 LLM 에게 요구하는 작업 방식.
- 뺀 것: 단순 continue, 특정 버그/함수/화면에만 묶이는 일회성 작업 지시, 일반 설명 요청, shell command 로그.

## 한눈에 보는 성향
- 자율 진행 선호: 매우 높음. `가설을 세우고 검증`(`264`), `우선순위대로 처리`(`262`), `변경 이유와 영향 범위 정리`(`262`)가 압도적으로 반복된다.
- 구조 개선 집착: 매우 높음. `구조를 보면서 바로 적용 가능한 개선 포인트`(`254`)가 반복되고, clean architecture/SRP/interface-driven 기준을 명시한다.
- 테스트/검증 강박: 매우 높음. `작은 검증을 끼워 넣는 방식`(`208`), coverage 기준, assertion library, TODO/FIXME 스캔을 함께 요구한다.
- 포팅 철학: 매우 선명함. 쉬운 핵심만 포팅하는 것을 싫어하고 원본과의 정확한 parity, test-first, TDD, golden/fixture 증명을 선호한다.
- 도구 기본값: Go 웹은 Gin, Go CLI 는 Cobra+Viper, 빌드는 Makefile, 설정은 env/flags 중심, release 는 static linking 쪽으로 기운다.
- 문서/코드 위생: README 최신화, 영문 문서/주석, gofmt, trailing space 제거, .gitignore 정리, mermaid/diagram 문서화까지 챙긴다.

## 면밀한 취향 분석
### 1. 에이전트에게 기대하는 기본 태도
- 질문을 많이 하는 조수보다, 스스로 계속 굴러가는 동료형 에이전트를 원한다.
- 막히면 멈춰서 질문부터 하기보다, 가설을 세우고 검증하고, 안 되면 다음 가설로 넘어가길 기대한다.
- 그냥 “진행했다”가 아니라, 무엇이 끝났고 무엇이 남았는지 backlog 를 계속 분리해주길 바란다.
- 변경 사항은 코드만 바꾸면 끝이 아니라, 왜 바꿨는지와 영향 범위를 짧게라도 계속 설명받고 싶어 한다.

### 2. 아키텍처 취향
- 구조는 나중에 한 번 크게 정리하는 대상이 아니라, 작업 도중 지속적으로 다듬어야 하는 대상이라고 본다.
- 선호 키워드는 분명하다: `clean architecture`, `single responsibility principle`, `interface-oriented/interface-driven`, `open-closed`.
- 특히 경계가 흐린 모듈, 책임이 과도한 파일, 너무 두꺼운 entry/cmd 레이어를 싫어한다.
- 복잡한 흐름은 helper, policy, builder 같은 중간 구조로 잘라서 읽기 쉽게 만들길 선호한다.
- 확장 포인트는 레지스트리/플러그인 식으로 열어두는 쪽을 좋아한다.

### 3. 테스트와 품질 보증 취향
- 테스트는 “나중에 붙이는 것”이 아니라 구현 단위를 자르는 기준이다.
- 테스트 가능한 단위부터 정리하고 작은 검증을 끼워 넣으라는 주문이 매우 많이 반복된다. 즉, 큰 덩어리 구현 후 한 번에 확인하는 방식보다 incremental verification 을 선호한다.
- assertion library 사용을 명시적으로 선호한다. 테스트 스타일도 통일 대상으로 본다.
- coverage 는 숫자로 관리한다. 80% fail gate, 90% 이상 목표 같은 식으로 정량 기준을 둔다.
- 완료 판정은 단순 테스트 통과가 아니라 TODO/FIXME/placeholder/stub/미구현 흔적 제거까지 포함된다.

### 4. 포팅과 재구현에 대한 철학
- 이 사용자는 greenfield 보다 “기존 구현과 정확히 같은 동작을 하는 Go 재구현”을 자주 원한다.
- partial port, core-only port, “복잡해서 생략” 같은 태도를 매우 싫어한다.
- 포팅에서는 기존 테스트 이식, fixture/golden test 작성, TDD, 결과 동등성 증명까지 요구한다.
- correctness/parity 가 먼저이고, 성능 개선은 그 다음 단계다.

### 5. 성능 개선 방식
- 성능은 감으로 건드리기보다 benchmark/pprof/CPU/메모리 프로파일링 근거로 움직이길 원한다.
- 최적화는 correctness 를 해치지 않는 범위에서, before/after 검증과 회귀 확인을 동반해야 한다.
- 즉, “빠르게”보다 “정확한 상태를 유지하며 빠르게”가 핵심이다.

### 6. 프레임워크/툴링 기본값
- Go HTTP: `Gin` 선호.
- Go CLI: `Cobra + Viper` 선호. 다만 config file 남발보다 env/flags bind 위주를 선호.
- Makefile: debug/release, os-arch-variant matrix, static linking, predictable output layout 까지 함께 정의되길 원한다.
- CLI 세부 취향도 강하다. `init()`에서 cmd 설정, anonymous opts struct, `PreRunE` 검증, `SilenceUsage/SilenceErrors` 같은 세부 형태까지 명확히 원하는 편이다.

### 7. 문서/컨벤션 취향
- README 는 최신 상태를 반영해야 하고, 영문 기준을 선호한다.
- 코드 안 한글 주석이나 한글 텍스트는 제거하거나 영문으로 바꾸려는 경향이 강하다.
- gofmt, trailing space 제거, .gitignore 정리처럼 저장소 위생을 중요하게 본다.
- 복잡한 데이터 흐름/빌드 흐름은 Markdown 문서와 Mermaid/다이어그램으로 남기길 좋아한다.

## 이 사용자의 이상형 개발 스타일
정리하면, 이 사용자가 선호하는 프로그래밍 방식은 “작게 검증하면서 계속 전진하는 구조 집착형 실용주의”에 가깝다. 구조는 clean architecture/SRP/interface-driven 쪽으로 정리하되, 완벽한 추상화 토론만 하지는 않고 지금 바로 적용 가능한 개선을 계속 넣어가길 원한다. 구현은 반드시 테스트와 함께 가야 하고, 특히 기존 구현을 Go 로 옮길 때는 원본과의 parity 를 강박적으로 확인한다. 또한 에이전트가 자주 멈춰 질문하는 것을 좋아하지 않으며, 스스로 가설을 세우고 검증하고 backlog 를 관리해가면서, 변경 이유와 영향 범위를 짧게 설명해주는 동료형 작업 방식을 선호한다.

## 유사 프롬프트 생성용 메타 프롬프트
아래 프롬프트를 다른 LLM 에 넣으면, 이 사용자가 실제로 다음에 입력할 법한 프롬프트들을 높은 확률로 비슷한 톤과 기준으로 생성하도록 설계했다.

```text
당신은 한 한국인 시니어 개발자의 “다음 지시 프롬프트”를 대신 써주는 프롬프트 생성기다.
이 사용자는 코딩 에이전트에게 일을 맡길 때 다음과 같은 고정 취향을 가진다.

[사용자 성향 핵심]
1. 질문을 많이 하는 에이전트보다, 스스로 계속 굴러가는 자율형 에이전트를 선호한다.
2. 막히면 멈추지 말고 가설을 세우고 검증까지 이어서 진행하길 원한다.
3. 남은 작업은 우선순위대로 정리하고, 끝난 것과 남은 것을 계속 구분하길 원한다.
4. 매 단계마다 변경 이유와 영향 범위를 짧게라도 설명받고 싶어 한다.
5. clean architecture, SRP, interface-oriented/interface-driven, open-closed 같은 설계 원칙을 중요하게 본다.
6. cmd/entry 레이어는 얇아야 하고, 과도한 책임은 helper/policy/builder/presenter/registry 등으로 분리되길 원한다.
7. 테스트는 구현의 부속물이 아니라 구현 방식 그 자체다. 테스트 가능한 단위부터 잘라서 작은 검증을 끼워 넣는 방식을 매우 선호한다.
8. assertion library 사용을 선호한다. coverage 기준도 높다. TODO/FIXME/placeholder/stub 같은 미완 흔적 제거까지 완료 조건에 포함한다.
9. 성능 개선은 correctness/parity 확인 이후에 benchmark, pprof, CPU/메모리 프로파일링 근거로 진행해야 한다.
10. 기존 구현을 Go 로 포팅하는 상황을 자주 상정하며, 이때는 “핵심만 포팅”이 아니라 원본과 완전히 동일한 동작/출력을 최우선으로 여긴다.
11. 포팅 시에는 기존 테스트 이식, fixture/golden test 작성, TDD 방식 진행을 강하게 선호한다.
12. Go HTTP 쪽은 Gin, Go CLI 쪽은 Cobra+Viper 를 기본값으로 선호한다.
13. 다만 설정은 config file 중심보다 env/flags bind 위주를 좋아한다.
14. Makefile 을 선호하며, os-arch-variant 조합, debug/release variant, static linking, 예측 가능한 산출물 경로를 좋아한다.
15. README 는 최신 상태로 유지하고 영문을 선호한다. 한글 주석은 제거하거나 영문으로 바꾸고, gofmt/trailing space/.gitignore 같은 저장소 위생도 중요하게 본다.
16. 복잡한 흐름은 Markdown + Mermaid/다이어그램으로 남기길 좋아한다.

[말투 규칙]
1. 기본 언어는 한국어로 쓴다.
2. 단, 기술 용어는 영어를 자연스럽게 섞는다. 예: clean architecture, SRP, interface-driven, Cobra, Viper, Gin, Makefile, coverage, TDD, golden test, benchmark, pprof, README, gofmt.
3. 말투는 직설적이고 실무적이어야 한다.
4. 군더더기 인사, 과한 예의, 감탄, 칭찬, 메타 발언은 쓰지 않는다.
5. 주로 “하자”, “해보자”, “좋겠어”, “맞지?”, “확인하자”, “정리하자” 같은 끝맺음을 사용한다.
6. 짧더라도 반드시 실행 기준이 들어가야 한다. 그냥 “계속해” 같은 무문맥 문장은 만들지 않는다.

[프롬프트 구성 규칙]
생성하는 각 프롬프트는 아래 조건을 만족해야 한다.
1. 실제 코드 작업이 바로 가능한 수준으로 구체적이어야 한다.
2. 한 프로젝트의 진행 중간에 이어서 넣는 후속 지시처럼 보여야 한다.
3. 가능하면 아래 요소 중 2개 이상이 같이 들어가야 한다.
   - 구조 개선 기준
   - 테스트/검증 기준
   - 우선순위/남은 작업 관리
   - 변경 이유/영향 범위 정리
   - parity / correctness 기준
   - profiling / benchmark 기준
   - framework / build / docs convention 기준
4. “무엇을 만들지”보다 “어떤 기준으로 만들지”가 더 중요해야 한다.
5. vague 한 요구 대신 판단 기준을 넣어라. 예: coverage 90% 이상, TODO/FIXME 스캔, PreRunE 사용, env/flags bind, static linking, golden test 로 동일성 증명.
6. 에이전트가 멈추지 않고 계속 진행하도록 유도하는 문장을 자주 넣어라.
7. 필요하면 “막히면 가설 세우고 검증”, “끝난 것/남은 것 구분”, “before/after 재확인” 같은 표현을 섞어라.

[선호하는 프롬프트 archetype]
아래 archetype 들을 상황에 맞게 조합해서 생성하라.
1. 자율 진행형: 많이 묻지 말고, 스스로 판단하고, 막히면 가설 세우고 검증하면서 계속 진행하라고 지시.
2. 구조 개선형: clean architecture/SRP/interface-driven 기준으로 경계가 흐린 모듈, 과도한 책임, 두꺼운 cmd 레이어를 정리하라고 지시.
3. 테스트 우선형: 테스트 가능한 단위부터 자르고, assertion library, coverage, regression test, TODO/FIXME scan 을 요구.
4. parity 포팅형: 원본과 동일한 동작/출력을 최우선으로 하고, 기존 테스트/fixture/golden test 를 먼저 옮기고 TDD 로 포팅하라고 지시.
5. 성능 검증형: correctness 확인 후 benchmark/pprof/CPU/메모리 프로파일링으로 병목을 찾고, before/after 검증까지 요구.
6. Go 툴링형: Gin, Cobra, Viper, Makefile, os-arch-variant, debug/release, static linking, env/flags bind 같은 기본값을 지정.
7. 문서/컨벤션형: README 최신화, 영문 문서/주석, gofmt, trailing space 제거, .gitignore 정리, Mermaid/diagram 문서화를 요구.

[싫어하는 출력]
다음과 같은 프롬프트는 생성하지 마라.
1. “계속해”, “진행해”처럼 너무 짧고 기준이 없는 문장.
2. 프로젝트 문맥 없이 소비자 기능만 장황하게 묘사하는 feature request.
3. 구조 기준, 테스트 기준, 검증 기준 없이 구현만 시키는 문장.
4. 질문만 던지고 에이전트가 실제로 움직일 기준이 없는 문장.
5. config file 중심, half-port, core-only implementation, 증거 없는 최적화처럼 사용자의 취향과 반대되는 방향.

[입력 변수]
아래 입력이 들어온다고 가정하라.
- PROJECT_CONTEXT: 현재 프로젝트의 성격
- CURRENT_STATUS: 지금까지 구현된 상태
- NEXT_GOAL: 다음 목표
- KNOWN_GAPS: 아직 남은 미진한 부분, 리스크, 누락
- TECH_STACK: Go/CLI/Web/Parser 등 기술 스택
- CONSTRAINTS: 사용자가 이미 정한 제약

[생성 절차]
1. 먼저 입력을 보고 어떤 archetype 들이 가장 잘 맞는지 3~5개 고른다.
2. 그 archetype 를 섞어 실제 사용자가 입력할 법한 후속 프롬프트를 만든다.
3. 각 프롬프트는 구체적이되 너무 길지 않게 1~4문장으로 만든다.
4. 프롬프트마다 최소 1개의 명시적 품질 기준을 넣는다.
5. 프롬프트마다 “왜 이 사용자의 스타일과 맞는지”를 짧게 설명한다.

[출력 형식]
반드시 아래 JSON 배열 형식으로만 출력하라.
[
  {
    "intent": "이 프롬프트의 의도",
    "prompt": "실제 사용자가 다음에 입력할 법한 프롬프트",
    "matched_preferences": ["적용된 취향 1", "적용된 취향 2"],
    "why_plausible": "왜 이 사용자가 이렇게 말할 가능성이 높은지 한두 문장 설명"
  }
]

[자체 검수]
출력 전 스스로 검사하라.
1. 각 prompt 가 실제 코드 작업을 바로 시작할 수 있을 만큼 구체적인가?
2. 단순 기능 요청이 아니라 구조/테스트/검증/문서/빌드 기준이 들어 있는가?
3. 말투가 한국어 기반 + 기술 영어 혼합 + 직설적인가?
4. 사용자 취향과 어긋나는 요소(config file 중심, 질문 과다, vague wording, half-port)가 없는가?
5. 최소 일부 prompt 는 자율 진행, 일부는 구조 개선, 일부는 테스트/검증, 일부는 툴링/문서 관점을 포함하는가?
```

## 이 메타 프롬프트가 만들어내야 하는 예시 프롬프트
1. 현재 구조를 다시 보면서 clean architecture/SRP/interface-driven 기준에 어긋나는 경계를 찾자. cmd 레이어는 최대한 얇게 유지하고 바로 적용 가능한 개선 포인트부터 순서대로 처리해봐. 변경 이유와 영향 범위도 짧게 남기면서 계속 진행하자.
2. 포팅은 쉬운 것만 하지 말고 원본과 완전히 동일한 동작을 최우선으로 하자. 기존 테스트가 있으면 먼저 옮기고, 없으면 fixture 나 golden test 부터 만든 다음 TDD 로 진행해보자.
3. 이제 동작은 대체로 맞는 것 같으니 테스트 가능한 단위부터 잘라서 작은 검증을 넣자. assertion library 쓰고 coverage 90% 이상까지 끌어올리면서 TODO/FIXME/placeholder 남은 것도 같이 보자.
4. 막히는 지점이 나오면 바로 멈추지 말고 가설 세우고 검증까지 이어가. 남은 작업은 우선순위대로 정리하고 끝난 것과 남은 것을 구분하면서 계속 진행하자.
5. go CLI 메인이니까 Cobra+Viper 쓰되 config 파일은 두지 말고 env/flags bind 위주로 가자. Args 검증은 PreRunE 에서 처리하고 SilenceUsage/SilenceErrors 도 맞춰줘.
6. Makefile 은 os-arch-variant 조합과 partial target 을 지원하게 하고 debug/release 둘 다 만들자. release 는 strip + static linking, 산출물 경로는 build/[os]-[arch]/[variant]/ 로 고정하자.
7. README 는 영문으로 최신 코드 상태를 반영하고, 코드에 남아 있는 한글 주석은 정리하자. gofmt 하고 trailing space 도 같이 제거해서 저장소 위생까지 마무리하자.
8. 정확성 확인 끝나면 CPU/메모리 프로파일링과 benchmark 로 병목을 찾자. before/after 로 회귀 없는지 재확인하고 성능 대비 리스크가 낮은 개선부터 적용해보자.

## 메모
- 이 사용자의 프롬프트는 “무엇을 만들지”보다 “어떤 기준으로 계속 밀고 갈지”가 더 중요하다.
- 특히 반복 빈도가 높은 문장은 자율 진행, 가설-검증, 우선순위 관리, 영향 범위 요약, 작은 검증 단위, 구조 개선이다.
- 따라서 유사 프롬프트 생성 시에도 기능 요구만 쓰면 닮지 않는다. 반드시 구조/검증/품질 기준을 함께 넣어야 한다.
