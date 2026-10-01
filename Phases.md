# quanto — 페이즈 정의

이 파일은 CLAUDE.md 22절의 짧은 지시("Phase N", "검수 Phase N" 등)가 가리키는 원문이다. 각 페이즈의 코드 블록 내용이 그 페이즈의 작업 지시 전체다.

## Phase 1 — 기반과 core/source

```
CLAUDE.md를 처음부터 끝까지 읽어라. 이번 세션은 Phase 1만 수행한다.

[절대 조건]
- git은 CLAUDE.md 0절 1번을 따른다. 산출물 묶음이 테스트를 통과할 때마다 `phase N: ...`로 커밋하고 작업 브랜치에 푸시한다. main에 직접 푸시하거나 force 푸시하지 마라. PR은 만들지 마라.
- CLAUDE.md 0절의 절대 규칙을 전부 지킨다. 특히 코드 주석 금지, 플레이스홀더 금지, 범위 고정, 의존성 허용 목록.

[선행 확인 — 하나라도 실패하면 즉시 멈추고 보고]
1. `go version`이 1.23 이상인가.
2. `git remote get-url origin`이 성공하는가. 결과에서 모듈 경로를 도출한다(CLAUDE.md 3절). 클라우드 세션의 원격 URL이 프록시 주소 형태라 owner/repo를 확정할 수 없으면 추측하지 말고 멈춰서 모듈 경로를 물어라.
3. 루트에 CLAUDE.md가 있는가. 없으면 멈춰라.
4. 추적 중인 파일이 LICENSE와 CLAUDE.md뿐인가(`git ls-files`). 다른 파일이 있으면 멈추고 목록을 보고한다.
5. 작업 브랜치를 확정한다. 세션 환경이 지정한 브랜치가 있으면 그것을 쓴다. 없고 현재 브랜치가 main이면 `git checkout -b phase-1`로 만든다. 브랜치 이름을 보고서에 적는다.

[이번 페이즈의 산출물]
1. go.mod, go.sum (의존성은 gopkg.in/yaml.v3만)
2. .gitignore — CLAUDE.md 19절의 항목만
3. core/source 패키지 전체 — CLAUDE.md 4절의 API, 불변식 14개, 필수 테스트 전부
4. core/imports_test.go — CLAUDE.md 2절의 의존 방향 표를 강제한다. 아직 없는 패키지 디렉터리는 검사 대상에서 자연스럽게 빠지게 구현한다.
5. scripts/fetch-corpus.sh — CLAUDE.md 20절
6. scripts/check-comments.go — CLAUDE.md 20.1절
7. Makefile — 이번 페이즈에 대상이 존재하는 타깃만: fmt-check, vet, comments, test, test-race, fuzz(FuzzLoad), corpus, check
8. .github/workflows/ci.yml — test 잡만. 퍼즈 단계는 FuzzLoad만. integration 잡은 Phase 7에서 추가한다.

[구현 지침]
- 4.2의 종료 위치 계산을 문장 그대로 구현한다. 특히 두 가지를 반드시 지킨다.
  (a) 블록 스칼라는 Value의 개행 수를 세지 않는다. 폴딩(>)은 개행을 공백으로 접으므로 틀린다. 들여쓰기 스캔으로 계산한다.
  (b) 모든 Position을 문서 범위로 클램프한다. `push:` 같은 null 값은 yaml.v3가 줄 끝 너머 컬럼을 보고한다.
- 컬럼은 rune 기준이다. 바이트로 계산하면 한글 테스트가 실패해야 정상이다. 한글 테스트가 이 차이를 실제로 검출하는지 확인한다.
- 머지 키(<<)와 별칭 해석은 4.2의 7~9번 규칙대로 구현한다. Walk는 별칭을 따라가지 않는다.

[코퍼스]
- scripts/fetch-corpus.sh를 실행한다. 실제로 30개 이상 받아지는지 확인한다.
- miss가 난 URL은 현재 존재하는 다른 유명 공개 저장소의 워크플로 URL로 교체하고 다시 실행한다. 대형 매트릭스, 재사용 워크플로, 앵커, 스케줄, 서비스 컨테이너, 한글 또는 비ASCII 문자열이 들어간 파일이 각각 최소 하나씩 포함되게 목록을 구성한다.

[검증 — 전부 통과해야 페이즈 종료]
- gofmt -l . 결과가 비어 있다
- go vet ./...
- go run scripts/check-comments.go
- go test -race ./...
- go test -run Corpus -v ./core/source/ 로 코퍼스 파일 수와 결과를 확인한다
- go test -run '^$' -fuzz FuzzLoad -fuzztime 60s ./core/source/
- make check

[종료]
CLAUDE.md 0절 1번에 따라 main으로 PR을 열고(병합하지 않는다), CLAUDE.md 21절 형식으로 보고하고 멈춰라. 다음 페이즈를 시작하지 마라.
```

---

## Phase 2 — core/expr, core/model

```
CLAUDE.md를 처음부터 끝까지 읽어라. 이번 세션은 Phase 2만 수행한다.

[절대 조건]
- git은 CLAUDE.md 0절 1번을 따른다. 산출물 묶음이 테스트를 통과할 때마다 `phase N: ...`로 커밋하고 작업 브랜치에 푸시한다. main에 직접 푸시하거나 force 푸시하지 마라. PR은 만들지 마라.
- CLAUDE.md 0절의 절대 규칙을 전부 지킨다.
- Phase 1 산출물(core/source 등)은 이 페이즈의 요구를 충족하는 데 반드시 필요한 경우에만 수정한다. 수정했다면 보고서에 이유를 적는다.

[선행 확인]
- CLAUDE.md 22.1절로 이전 작업을 이어받는다. 그 뒤 `git log --oneline -40`에 `phase 1:` 커밋이 없으면 멈추고 보고한다.
- make check가 현재 상태에서 통과하는지 먼저 확인한다. 실패하면 아무것도 만들지 말고 실패 내용을 보고한다.

[이번 페이즈의 산출물]
1. core/expr — CLAUDE.md 5절 전부. 평가기는 만들지 않는다.
2. core/model — CLAUDE.md 6절 전부. SecretRefs 수집 포함.
3. Makefile fuzz 타깃과 CI 퍼즈 단계에 FuzzParseTemplate 추가.

[구현 지침]
- expr 렉서는 rune 단위로 동작하고 Offset은 rune 오프셋이다.
- 문자열 리터럴 안의 }}를 템플릿 종료로 오인하지 않는다. 테스트로 증명한다.
- model은 source.Node API만 사용한다. yaml.v3를 직접 import하지 않는다.
- model의 모든 map 필드는 결정적으로 사용될 수 있도록, 순서가 의미 있는 곳(EnvKeys, Services, SecretNames, Outputs, Inputs, SecretRefs)은 정렬된 슬라이스로 둔다.
- 액션 참조 파싱 규칙(6.2)의 모든 분기를 테이블 테스트로 덮는다.
- 코퍼스 전체에 대해 model.Parse가 error 없이 끝나는지 검사하는 TestCorpusModel을 추가한다. 진단(Diagnostic)은 허용되지만, 코퍼스 파일에서 나온 진단 코드별 개수를 테스트 로그로 출력한다. MODEL-STEP-NO-ACTION이나 MODEL-EXPR-SYNTAX가 코퍼스에서 나오면 파서 버그일 가능성이 높으니 원인을 조사하고 보고서에 적는다.

[검증]
- make check
- go test -run Corpus -v ./core/...
- go test -run '^$' -fuzz FuzzParseTemplate -fuzztime 60s -fuzzminimizetime 5s ./core/expr/

[종료]
CLAUDE.md 0절 1번에 따라 main으로 PR을 열고(병합하지 않는다), CLAUDE.md 21절 형식으로 보고하고 멈춰라.
```

---

## Phase 3 — core/matrix, core/graph

```
CLAUDE.md를 처음부터 끝까지 읽어라. 이번 세션은 Phase 3만 수행한다.

[절대 조건]
- git은 CLAUDE.md 0절 1번을 따른다. 산출물 묶음이 테스트를 통과할 때마다 `phase N: ...`로 커밋하고 작업 브랜치에 푸시한다. main에 직접 푸시하거나 force 푸시하지 마라. PR은 만들지 마라.
- CLAUDE.md 0절의 절대 규칙을 전부 지킨다.

[선행 확인]
- CLAUDE.md 22.1절로 이전 작업을 이어받는다. 그 뒤 `git log --oneline -40`에 `phase 2:` 커밋이 없으면 멈추고 보고한다.
- make check가 통과하는지 먼저 확인한다. 실패하면 멈추고 보고한다.

[이번 페이즈의 산출물]
1. core/matrix — CLAUDE.md 7절 전부
2. core/graph — CLAUDE.md 8절 전부
3. Makefile fuzz 타깃과 CI 퍼즈 단계에 FuzzExpand 추가

[구현 지침 — 매트릭스가 제품 정확성의 급소다]
- 7.2의 include 규칙을 문장 그대로 구현한다. 핵심은 "병합 후보는 exclude 적용 후의 기본 조합뿐이며, include가 새로 만든 조합은 이후 include의 병합 후보가 아니다"이다.
- 7.3의 GitHub 문서 예제를 첫 번째 테스트로 작성하고, 기대값 6개를 순서까지 정확히 맞춘다. 이 테스트가 통과하기 전에는 다른 테스트를 작성하지 않는다.
- 조합 순서: 첫 번째 축이 가장 바깥 루프다. 축 순서는 YAML 선언 순서다(map 순회 금지).
- 값 동등성: 키 정렬 정규 JSON 비교. 정수 18과 문자열 "18"은 다르다. 결정 사항에 기록한다.
- 곱이 MaterializeLimit을 넘으면 곱 계산 자체에서 오버플로가 나지 않게 한다(각 단계에서 상한 초과 시 중단).
- graph의 모든 출력(Jobs, Cycles, Unresolved, map 값의 슬라이스)은 정렬된 상태로 반환한다.

[추가 검증]
- 코퍼스의 모든 잡에 대해 matrix.Expand를 돌리는 TestCorpusMatrix를 추가한다. error가 없어야 한다. 테스트 로그에 파일별 총 인스턴스 수와 동적 매트릭스 개수를 출력한다.
- 코퍼스의 모든 워크플로에 대해 graph.Build를 돌리는 TestCorpusGraph를 추가한다. Cycles와 Unresolved가 비어 있어야 한다(실제 운영 중인 워크플로이므로). 비어 있지 않으면 구현 버그로 간주하고 조사한다.

[검증]
- make check
- go test -run Corpus -v ./core/...
- go test -run '^$' -fuzz FuzzExpand -fuzztime 60s -fuzzminimizetime 5s ./core/matrix/

[종료]
CLAUDE.md 0절 1번에 따라 main으로 PR을 열고(병합하지 않는다), CLAUDE.md 21절 형식으로 보고하고 멈춰라.
```

---

## Phase 4 — core/semdiff

```
CLAUDE.md를 처음부터 끝까지 읽어라. 이번 세션은 Phase 4만 수행한다. 이 페이즈는 제품의 핵심이다.

[절대 조건]
- git은 CLAUDE.md 0절 1번을 따른다. 산출물 묶음이 테스트를 통과할 때마다 `phase N: ...`로 커밋하고 작업 브랜치에 푸시한다. main에 직접 푸시하거나 force 푸시하지 마라. PR은 만들지 마라.
- CLAUDE.md 0절의 절대 규칙을 전부 지킨다.

[선행 확인]
- CLAUDE.md 22.1절로 이전 작업을 이어받는다. 그 뒤 `git log --oneline -40`에 `phase 3:` 커밋이 없으면 멈추고 보고한다.
- make check가 통과하는지 먼저 확인한다. 실패하면 멈추고 보고한다.

[진행 방식]
구현 전에 다음 계획을 먼저 제시하고 내 승인을 기다려라.
- 파일 분할 계획(비교 영역별 파일)
- 잡 매칭(rename 포함) 알고리즘 의사 코드
- 26개 골든 케이스 각각의 before/after 요지와 기대 Finding Kind 목록
승인 후 구현한다.

[이번 페이즈의 산출물]
1. core/semdiff — CLAUDE.md 9절 전부
2. testdata/golden/semdiff/<case>/ — 9.8의 26개 케이스, 각각 before.yml, after.yml, expected.json
3. semdiff 타입과 source.Position의 json 태그(9.1 직렬화 규칙)
4. Makefile fuzz 타깃과 CI 퍼즈 단계에 FuzzCompare 추가

[구현 지침]
- 9.4 표의 Kind 이름과 영어 문구를 한 글자도 바꾸지 않는다. Kind 순서 상수 표를 코드에 하나만 두고 정렬과 문서화에 공유한다.
- 문구 렌더링은 Phase 5의 report.Message 몫이다. 이번 페이즈의 Finding에는 Kind, Subject, Before, After, Detail 필드만 채운다.
- reformatted 케이스(Finding 0개)가 가장 중요한 음성 테스트다. 플로우↔블록, 따옴표 스타일, 주석, 키 순서, 같은 값을 앵커·별칭으로 바꾸기를 한 파일에 모두 섞는다.
- anchor-shared-change 케이스는 `runs-on: &runner` 정의 한 줄 변경이 두 잡의 job.runner_changed 두 건으로 나와야 한다.
- GitHub Actions는 머지 키(`<<`)를 거부하므로 골든 케이스와 fixture에 머지 키를 쓰지 않는다.
- 골든 expected.json은 처음 한 번 -update로 생성한 뒤, 각 파일을 직접 열어 명세와 대조해 검토한다. 명세와 다르면 골든이 아니라 구현을 고친다. 검토한 내용을 보고서의 "골든 파일 갱신"에 케이스별 한 줄로 적는다.
- 속성 테스트(9.8 마지막 문단)를 반드시 구현한다. 코퍼스 N개 파일에서 Compare(a, a)가 전부 Finding 0개이고, 인접 파일 쌍 (a, b)에 대해 대칭성 검사를 수행한다.
- CronRunsPerDay는 별도 테이블 테스트를 둔다: '0 * * * *'=24, '*/15 * * * *'=96, '0 0 * * *'=1, '0 9 * * 1-5'=1 매일아님, '30 2,14 * * *'=2, 'MON'이 들어간 식=해석실패, 필드 4개=해석실패.

[검증]
- make check
- go test -run Corpus -v ./core/semdiff/
- go test -run '^$' -fuzz FuzzCompare -fuzztime 60s -fuzzminimizetime 5s ./core/semdiff/

[종료]
CLAUDE.md 0절 1번에 따라 main으로 PR을 열고(병합하지 않는다), CLAUDE.md 21절 형식으로 보고하고 멈춰라.
```

---

## Phase 5 — core/report, CLI

```
CLAUDE.md를 처음부터 끝까지 읽어라. 이번 세션은 Phase 5만 수행한다.

[절대 조건]
- git은 CLAUDE.md 0절 1번을 따른다. 산출물 묶음이 테스트를 통과할 때마다 `phase N: ...`로 커밋하고 작업 브랜치에 푸시한다. main에 직접 푸시하거나 force 푸시하지 마라. PR은 만들지 마라.
- CLAUDE.md 0절의 절대 규칙을 전부 지킨다.

[선행 확인]
- CLAUDE.md 22.1절로 이전 작업을 이어받는다. 그 뒤 `git log --oneline -40`에 `phase 4:` 커밋이 없으면 멈추고 보고한다.
- make check가 통과하는지 먼저 확인한다. 실패하면 멈추고 보고한다.

[이번 페이즈의 산출물]
1. core/report — CLAUDE.md 10절 전부
2. testdata/golden/semdiff/<case>/expected.md, expected.txt — 26개 케이스 전부
3. internal/cli — version, inspect, diff (CLAUDE.md 11절). serve, migrate, manifest는 만들지 않는다.
4. cmd/quanto/main.go — internal/cli.Run 호출과 종료 코드 처리만
5. Makefile build 타깃 (bin/quanto, -ldflags로 version 주입)

[구현 지침]
- Markdown 출력은 10절의 예시와 구조가 바이트 단위로 같아야 한다(값만 다름). 예시를 그대로 재현하는 테스트를 하나 둔다.
- MaxBodyRunes 절단은 파일 단위다. 경계 테스트를 둔다(한계 바로 아래, 바로 위, 파일 하나가 단독으로 한계를 넘는 경우).
- Annotations는 Pos가 유효한 Finding만 대상이며, 여러 줄 span이면 컬럼을 0으로 둔다. GitHub API가 여러 줄 어노테이션의 컬럼을 거부하기 때문이다.
- CLI는 internal/cli.Run(args, stdout, stderr) int로 구현하고 테스트는 이 함수를 직접 호출한다. os.Exit는 cmd/quanto에서만.
- diff 명령에서 /dev/null과 빈 파일은 "부재"로 취급한다. 종료 코드는 CLAUDE.md 11절을 따른다.

[실사용 검증 — 반드시 수행]
- make build 후 ./bin/quanto diff 를 다음 조합으로 실행하고 출력 일부를 보고서에 첨부한다.
  (a) 골든 케이스 matrix-axis-added의 before/after, --format markdown
  (b) 코퍼스에서 서로 다른 두 파일, --format text
  (c) 코퍼스 한 파일과 그 파일 자신, --format json (Finding 0개 확인)
  (d) /dev/null 과 코퍼스 한 파일 (workflow.added)
- ./bin/quanto inspect 를 코퍼스에서 가장 큰 파일에 실행해 출력 일부를 첨부한다.

[검증]
- make check
- make build

[종료]
CLAUDE.md 0절 1번에 따라 main으로 PR을 열고(병합하지 않는다), CLAUDE.md 21절 형식으로 보고하고 멈춰라.
```

---

## Phase 6 — config, github, metrics

```
CLAUDE.md를 처음부터 끝까지 읽어라. 이번 세션은 Phase 6만 수행한다.

[절대 조건]
- git은 CLAUDE.md 0절 1번을 따른다. 산출물 묶음이 테스트를 통과할 때마다 `phase N: ...`로 커밋하고 작업 브랜치에 푸시한다. main에 직접 푸시하거나 force 푸시하지 마라. PR은 만들지 마라.
- CLAUDE.md 0절의 절대 규칙을 전부 지킨다.
- 테스트에서 실제 GitHub API에 접근하지 마라. 전부 httptest.Server로 대체한다.

[선행 확인]
- CLAUDE.md 22.1절로 이전 작업을 이어받는다. 그 뒤 `git log --oneline -40`에 `phase 5:` 커밋이 없으면 멈추고 보고한다.
- make check가 통과하는지 먼저 확인한다. 실패하면 멈추고 보고한다.

[이번 페이즈의 산출물]
1. internal/config — CLAUDE.md 12절 전부
2. internal/github — CLAUDE.md 13절 전부
3. internal/metrics — CLAUDE.md 16절 전부
4. go.mod에 github.com/prometheus/client_golang 추가

[구현 지침]
- JWT는 crypto/rsa, crypto/sha256, encoding/base64(RawURLEncoding)로 구현한다. 테스트에서 rsa.GenerateKey로 키를 만들고 PKCS#1과 PKCS#8 PEM 양쪽으로 로딩을 검증한 뒤, 생성된 토큰의 서명을 공개키로 검증한다.
- 설치 토큰 캐시는 시계를 주입할 수 있게 만든다(now func() time.Time). 만료 5분 전 갱신을 가짜 시계로 테스트한다. 동시에 100개 고루틴이 같은 설치 토큰을 요청해도 발급 호출이 1회인지 검증한다.
- 레이트 리밋 두 형태(Retry-After, X-RateLimit-Reset)를 각각 테스트한다.
- 어노테이션 배치: 120개를 넘기면 create 1회(50개) + update 2회(50, 20)인지 가짜 서버에서 요청 본문으로 검증한다.
- 경로 이스케이프: 공백, #, 한글이 포함된 파일 경로로 FileContent를 호출하는 테스트를 둔다.
- 에러와 로그에 토큰, JWT, 개인키가 절대 포함되지 않는지 확인하는 테스트를 둔다(에러 문자열에 토큰 값이 없는지 검사).
- config의 String()이 시크릿을 [redacted]로 표시하는지 테스트한다.
- metrics는 prometheus.NewRegistry()를 쓰고 전역 레지스트리를 건드리지 않는다. github 패키지는 metrics를 import하지 않는다(OnResponse 훅으로 연결).

[검증]
- make check

[종료]
CLAUDE.md 0절 1번에 따라 main으로 PR을 열고(병합하지 않는다), CLAUDE.md 21절 형식으로 보고하고 멈춰라.
```

---

## Phase 7 — store, app, serve

```
CLAUDE.md를 처음부터 끝까지 읽어라. 이번 세션은 Phase 7만 수행한다.

[절대 조건]
- git은 CLAUDE.md 0절 1번을 따른다. 산출물 묶음이 테스트를 통과할 때마다 `phase N: ...`로 커밋하고 작업 브랜치에 푸시한다. main에 직접 푸시하거나 force 푸시하지 마라. PR은 만들지 마라.
- CLAUDE.md 0절의 절대 규칙을 전부 지킨다.
- 워크플로 파일 원문을 DB의 어떤 테이블에도 저장하지 마라.

[선행 확인]
- CLAUDE.md 22.1절로 이전 작업을 이어받는다. 그 뒤 `git log --oneline -40`에 `phase 6:` 커밋이 없으면 멈추고 보고한다.
- make check가 통과하는지 먼저 확인한다. 실패하면 멈추고 보고한다.

[진행 방식]
구현 전에 다음 계획을 먼저 제시하고 내 승인을 기다려라.
- 패키지 내부 파일 분할
- 웹훅 처리 순서도(서명 → 헤더 → DeliverySeen → 처리 → RecordDelivery)
- 워커 루프와 종료 절차
- analyze_pr 핸들러의 단계별 실패 시 동작 표(각 단계에서 에러가 나면 Fail인지 Defer인지 Complete인지)
승인 후 구현한다.

[테스트 DB — 클라우드 세션에서는 아래 순서로 시도한다]
1. 컨테이너 런타임(podman 또는 docker)이 동작하면 postgres:16 컨테이너를 127.0.0.1:55432로 띄운다.
2. 안 되면 패키지 관리자로 PostgreSQL 서버를 설치하고(`apt-get install -y postgresql` 등) 로컬 클러스터를 시작한다. root가 아니면 sudo 가능 여부를 확인한다.
3. 둘 다 안 되면 로컬 통합 테스트를 포기한다. 이 경우 통합 테스트의 정답은 푸시 후 GitHub Actions의 integration 잡 결과다. 보고서에 "로컬 DB 불가, CI integration 잡으로 검증 필요"라고 명시한다. 컴파일과 단위 테스트는 어떤 경우에도 전부 통과해야 한다.
- DB를 띄웠으면 `QUANTO_TEST_DATABASE_URL`을 설정하고, 준비될 때까지 연결 재시도로 기다린다.
- 어떤 방법을 썼는지 보고서의 결정 사항에 적는다.

[이번 페이즈의 산출물]
1. internal/store, internal/store/migrations/0001_init.sql — CLAUDE.md 14절 전부
2. internal/app — CLAUDE.md 15절 전부 (web, worker, 세 핸들러, e2e 테스트)
3. internal/cli에 serve(--role web|worker|all), migrate 추가
4. go.mod에 github.com/jackc/pgx/v5 추가
5. .github/workflows/ci.yml에 integration 잡 추가 (CLAUDE.md 18절)

[구현 지침]
- 큐 동시성 테스트(고루틴 8개 × 작업 100개)에서 각 작업이 정확히 한 번씩 Dequeue되는지 검증한다.
- 동시 마이그레이션 테스트는 서로 다른 pgxpool 두 개로 수행한다.
- analyze_pr 테스트의 가짜 GitHub는 요청 로그를 기록하고, 테스트는 호출된 엔드포인트 순서와 본문을 검증한다.
- head 이동 테스트: PullRequest 재조회에서 다른 head_sha를 돌려주면 Check Run은 생성되고 코멘트 API는 호출되지 않아야 한다.
- 코멘트 탐색은 마커와 봇 login이 모두 일치해야 한다. 마커만 같은 사람 코멘트를 수정하지 않는지 테스트한다.
- e2e 골든: testdata/golden/e2e/ 아래에 Check Run 요청 본문과 코멘트 본문을 저장한다. 워크플로 fixture는 semdiff 골든 케이스 중 matrix-axis-added를 재사용한다.
- web 핸들러에 대한 테스트는 store 없이 가능한 부분(서명, 헤더, 크기)을 단위 테스트로, 나머지는 통합 테스트로 둔다.

[실동작 검증 — 로컬 DB가 있을 때만]
- ./bin/quanto migrate 를 두 번 실행해서 두 번째가 아무것도 적용하지 않는지 확인한다.
- 테스트용 설정(가짜 App ID, 테스트용 RSA 키 파일, 웹훅 시크릿 파일, QUANTO_GITHUB_API_URL은 로컬 가짜 주소)으로 ./bin/quanto serve --role all 을 백그라운드로 띄우고, curl로 /healthz, /readyz, /metrics를 호출한 뒤, 올바르게 서명한 ping 웹훅과 잘못 서명한 웹훅을 보내 응답 코드를 확인한다. 끝나면 프로세스를 종료하고, 이때 사용한 임시 키와 시크릿 파일을 스크래치 경로에서 삭제한다. 저장소 안에 두지 않는다.

[검증]
- make check
- 로컬 DB가 있으면 QUANTO_TEST_DATABASE_URL 설정 상태에서 go test -race ./internal/...
- 푸시 후 CI의 integration 잡이 통과하는지 확인할 수 있으면 확인하고, 확인할 수 없으면 보고서에 사람이 확인해야 한다고 적는다

[종료]
CLAUDE.md 0절 1번에 따라 main으로 PR을 열고(병합하지 않는다), CLAUDE.md 21절 형식으로 보고하고 멈춰라.
```

---

## Phase 8 — 배포, 문서, 최종 감사

```
CLAUDE.md를 처음부터 끝까지 읽어라. 이번 세션은 Phase 8만 수행한다. 마지막 페이즈다.

[절대 조건]
- git은 CLAUDE.md 0절 1번을 따른다. 산출물 묶음이 테스트를 통과할 때마다 `phase N: ...`로 커밋하고 작업 브랜치에 푸시한다. main에 직접 푸시하거나 force 푸시하지 마라. PR은 만들지 마라.
- CLAUDE.md 0절의 절대 규칙을 전부 지킨다.

[선행 확인]
- CLAUDE.md 22.1절로 이전 작업을 이어받는다. 그 뒤 `git log --oneline -40`에 `phase 7:` 커밋이 없으면 멈추고 보고한다.
- make check가 통과하는지 먼저 확인한다. 실패하면 멈추고 보고한다.

[이번 페이즈의 산출물]
1. internal/cli에 manifest 명령 (CLAUDE.md 17절)
2. deploy/Containerfile, deploy/quadlet/ 5개 파일 (CLAUDE.md 17절)
3. Makefile image 타깃 (podman build)
4. docs/github-app.md, docs/deploy.md
5. README.md

[구현 지침]
- Containerfile의 golang 이미지 태그는 go.mod의 go 지시어와 일치시킨다.
- 컨테이너 빌드가 가능한 환경이면 make image 로 이미지를 실제로 빌드하고, 컨테이너로 version 명령을 실행해 버전 출력을 확인한다. 클라우드 세션에서 빌드가 불가능하면 보고서에 명시하고, Containerfile은 문법과 단계(빌드 스테이지 → distroless 복사)를 육안 검토한다.
- Quadlet 파일은 podman의 quadlet 생성기로 문법을 검증한다. /usr/libexec/podman/quadlet -dryrun -user 를 파일이 있는 디렉터리를 가리키게 해서 실행하고 오류가 없는지 확인한다(생성기가 없으면 각 키가 podman-systemd.unit 문서에 존재하는 키인지 대조하고 그 사실을 보고).
- README의 예시 코멘트는 손으로 쓰지 말고 ./bin/quanto diff --format markdown 을 골든 케이스에 실제로 실행한 출력을 붙여넣는다.
- README와 docs에는 명세에 없는 기능, 존재하지 않는 명령, 미래 계획을 쓰지 않는다.

[최종 감사 — 반드시 수행]
CLAUDE.md의 0절부터 21절까지 각 절을 순서대로 다시 읽고, 절마다 다음 표 한 행을 채운다.
| 절 | 요구 사항 요약 | 구현 위치 | 테스트 위치 | 상태(충족/부분/미충족) |
부분이나 미충족이 있으면 이번 페이즈 안에서 고친다. 고칠 수 없는 것은 이유와 함께 보고서의 "알려진 한계"에 적는다.

추가로 다음을 검사하고 결과를 보고한다.
- .md 파일을 제외한 저장소 전체에서 TODO, FIXME, XXX, "not implemented", panic( 의 사용처를 grep으로 전부 찾는다. core/와 internal/ 안의 panic은 0건이어야 한다. (CLAUDE.md는 금지어 목록으로 이 단어들을 포함하므로 검색 대상에서 뺀다)
- go run scripts/check-comments.go
- SQL, YAML, 셸, Makefile, Containerfile 파일에 주석이 없는지 육안으로 전수 확인한다(셸 shebang 제외).
- go mod graph로 직접 의존성이 허용 목록 4개뿐인지 확인한다.
- go build ./... 와 go test -race ./... 를 캐시 없이 실행한다: go clean -testcache 후.

[검증]
- make check
- make build
- make image (컨테이너 빌드가 가능할 때만)

[종료]
CLAUDE.md 0절 1번에 따라 main으로 PR을 열고(병합하지 않는다), CLAUDE.md 21절 형식에 최종 감사 표를 붙여 보고하고 멈춰라.
```

---

## 검수

```
CLAUDE.md를 처음부터 끝까지 읽어라. 너는 검수자다. 어떤 파일도 수정하지 말고, 커밋과 푸시도 하지 마라. 이 세션은 읽기 전용이다.

검수 대상은 브랜치 <브랜치>의 Phase N이다.
git fetch origin <브랜치> 후 git diff main...origin/<브랜치> 와 git log main..origin/<브랜치> 로 변경을 파악하라. 테스트 실행이 필요하면 git checkout --detach origin/<브랜치> 로 전환해서 실행한다.

다음을 검사하고 결과만 보고하라.
1. CLAUDE.md에서 Phase N 범위에 해당하는 절의 요구 사항을 항목별로 나열하고, 각각 충족/부분/미충족과 근거(파일:줄)를 적는다.
2. 명세에 없는 기능, 플래그, 파일, 의존성이 추가됐는지 찾는다.
3. 절대 규칙 위반을 찾는다: 코드 주석, 플레이스홀더, core/internal 안의 panic·os.Exit·log.Fatal, map 순회 결과를 정렬 없이 출력하는 곳, 에러 무시(_ = err 또는 반환값 버림), 비밀값이 로그나 에러에 들어갈 수 있는 경로, wip 커밋이 남아 있는지.
4. 테스트가 명세의 필수 테스트 목록을 전부 덮는지 대조한다. 빠진 테스트를 나열한다.
5. 골든 파일 중 무작위 3개를 골라 직접 열고, 명세를 손으로 적용한 결과와 일치하는지 검토한다.
6. make check를 실행하고 결과를 적는다.

출력 형식:
- 치명(병합 전 반드시 수정)
- 중요(다음 페이즈 전 수정 권장)
- 경미
각 항목은 "파일:줄 — 문제 — 명세 근거(절 번호)" 한 줄로 쓴다.
```
