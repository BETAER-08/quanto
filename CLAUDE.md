# quanto — 에이전트 작업 명세

이 문서는 저장소 루트에 둔다. 모든 작업 세션은 이 문서를 먼저 끝까지 읽는다. 이 문서와 개별 프롬프트가 충돌하면 이 문서가 우선한다.

---

## 0. 절대 규칙

1. **git 규칙 (main 단일 브랜치).** 이 작업은 Claude Code 클라우드 세션에서 수행하고, 모든 작업은 `main`에 직접 커밋·푸시한다. 브랜치와 PR은 쓰지 않는다. 세션이 끝나면 컨테이너가 사라지므로 **푸시하지 않은 작업은 유실된다.**
   - 작업 시작 시 `git fetch origin` → `git checkout main` → `git pull --ff-only origin main`으로 최신 `main`에서 시작한다. 세션 환경이 `claude/...` 브랜치를 만들어 두었어도 `main`으로 전환한다.
   - 산출물 묶음(패키지 하나, 골든 케이스 묶음 하나 등)이 해당 테스트를 통과할 때마다 커밋하고 `git push origin main`으로 푸시한다. 페이즈 끝에 한 번만 커밋하지 않는다.
   - 커밋 메시지는 `phase N: <영어 요약>` 형식이다.
   - 테스트가 실패하는 상태는 커밋하지 않는다. 예외: 세션 종료가 임박했을 때는 `phase N: wip <내용>`으로 커밋·푸시하고 보고서에 미완료 항목을 적는다.
   - 푸시가 거절되면(원격이 앞서 있음) `git pull --ff-only origin main` 후 테스트를 다시 돌리고 푸시한다. fast-forward가 안 되면 `git pull --no-rebase origin main`으로 병합하고, 충돌이 나면 `git merge --abort` 후 멈추고 보고한다.
   - force 푸시, `rebase`, `reset --hard`, 히스토리 재작성, 태그 생성을 하지 않는다. 브랜치를 만들지 않고 PR을 열지 않는다.
   - `main` 푸시가 권한 검사에 막히면 우회 경로(브랜치 푸시 후 PR 병합 등)를 시도하지 말고, 막힌 명령과 메시지를 보고하고 멈춘다.
2. **코드 주석 금지.** `//` 주석, `/* */` 주석, doc comment를 테스트 파일까지 포함해 전부 쓰지 않는다. SQL, 셸, YAML, Makefile, Containerfile 안의 주석도 금지다. 예외는 컴파일러 지시문 `//go:build`, `//go:embed`, `//go:generate`, 셸 스크립트 첫 줄의 shebang, 그리고 워크플로·액션 YAML에서 커밋 SHA로 고정한 `uses:` 줄 끝의 버전 주석(`# vX.Y.Z` 형식, 예: `uses: actions/checkout@<40자 SHA> # v7.0.1`)뿐이다. 이 버전 주석은 SHA로 고정한 모든 `uses:` 줄에 필수이며, 버전은 `git ls-remote --tags`로 SHA와 태그를 대조해 확정한다. 그 외 YAML 주석은 금지다. 마크다운 문서는 주석 규칙의 대상이 아니다. Go 파일은 `go run scripts/check-comments.go`가 기계적으로 검사한다(20.1절).
3. **플레이스홀더 금지.** `TODO`, `FIXME`, `XXX`, `panic("not implemented")`, 빈 함수 본문, "추후 구현" 류를 남기지 않는다. 현재 페이즈 범위 밖의 기능은 파일 자체를 만들지 않는다.
4. **범위 고정.** 이 문서에 없는 기능, 플래그, 환경변수, 의존성, 파일을 추가하지 않는다. 명세가 모호하면 가장 보수적인 해석을 택하고 페이즈 보고서의 "결정 사항"에 기록한다.
5. **의존성 허용 목록.** Go 표준 라이브러리, `gopkg.in/yaml.v3`, `github.com/jackc/pgx/v5`, `github.com/prometheus/client_golang`. 그 외 모듈은 테스트 용도라도 추가하지 않는다. GitHub API 클라이언트, JWT, 라우터, CLI 프레임워크, 테스트 어서션 라이브러리 전부 표준 라이브러리로 구현한다.
6. **LICENSE 파일 수정 금지.**
7. **테스트는 실제 네트워크에 접근하지 않는다.** 외부 HTTP는 `net/http/httptest`로 대체한다. 유일한 예외는 `scripts/fetch-corpus.sh`다.
8. **`core/`와 `internal/` 안에서 panic, `log.Fatal`, `os.Exit` 금지.** 전부 error로 반환한다. 종료 처리는 `cmd/`에서만 한다. `recover`는 워커 작업 루프 한 곳에서만 허용한다.
9. **결정성.** 입력이 같으면 모든 출력(진단 순서, 보고서, JSON, 마크다운)이 바이트 단위로 같아야 한다. map을 순회한 결과를 출력하거나 반환할 때는 반드시 정렬한다.
10. **검증을 우회하지 않는다.** 테스트를 삭제하거나 `t.Skip`을 추가하거나 기대값을 근거 없이 바꿔서 통과시키지 않는다. 골든 파일을 갱신했다면 보고서에 갱신한 파일과 그 근거를 적는다.
11. **에러 처리.** 래핑은 `fmt.Errorf("...: %w", err)`로 한다. 에러를 무시하지 않는다. 비밀값(개인키, 웹훅 시크릿, 토큰, DB 비밀번호)은 에러 메시지와 로그에 절대 포함하지 않는다.

---

## 1. 제품 정의

quanto는 GitHub App이자 GitHub Action(23절)이다. PR이 `.github/workflows/*.yml` 또는 `*.yaml`을 변경하면, 변경 전후 워크플로를 **실행 모델**로 해석해서 무엇이 달라지는지를 수치와 사실로 보고한다.

보고 대상의 예는 다음과 같다. 매트릭스 조합 수 6 → 24, `needs` 체인 길이, 최대 동시 잡 수, `permissions` 확대, 신규 서드파티 액션, SHA 고정 해제, 트리거 추가, 스케줄 빈도, 과거 실행 이력 기반 러너 시간 추정.

**어조 정책.** quanto는 린터가 아니라 계기판이다. "나쁘다", "위험하다", "해야 한다"라고 판단하지 않는다. 무엇이 얼마나 바뀌는지만 말한다. 머지를 차단하지 않는다. Check Run 결론은 항상 `neutral`이다.

**보안 정책.** PR 코드를 실행하지 않는다. 워크플로 파일 원문을 DB에 저장하지 않는다. 분석 결과 메타데이터만 저장한다.

**v1 범위 밖.** 워크플로 생성, 로컬 실행, 스키마 린팅(actionlint 영역), 저장소 파일 대조, 정책 엔진, 머지 차단, 결제, 대시보드, 비공개 저장소 기본 지원, 달러 단위 비용 환산.

---

## 2. 저장소 구조

```
cmd/quanto/                 main 패키지, 서브커맨드 라우팅
core/source/                YAML 로딩, 위치(span), 논리 경로
core/expr/                  ${{ }} 표현식 렉서·파서·참조 추출
core/model/                 AST → Workflow 실행 모델
core/matrix/                strategy.matrix 확장
core/graph/                 needs 그래프 분석
core/semdiff/               두 Workflow의 의미 차이
core/report/                텍스트·마크다운·JSON·어노테이션 렌더링
internal/cli/               서브커맨드 구현
internal/config/            환경변수 로딩·검증
internal/github/            GitHub REST 클라이언트, App JWT
internal/store/             PostgreSQL, 마이그레이션, 큐
internal/store/migrations/  *.sql
internal/app/               web 역할, worker 역할, 작업 핸들러
internal/analysis/          PR 워크플로 파일 읽기·비교 (App과 Action 공용, 23절)
internal/action/            quanto action 서브커맨드 (23절)
internal/metrics/           Prometheus 수집기
testdata/golden/            골든 파일
testdata/corpus/            실제 워크플로 (gitignore, 스크립트로 수집)
scripts/fetch-corpus.sh
scripts/check-comments.go
action.yml
deploy/Containerfile
deploy/.containerignore
deploy/quadlet/
docs/
.github/workflows/ci.yml
.github/workflows/release.yml
.github/workflows/quanto.yml
Makefile
```

**의존 방향 (강제).**

| 패키지 | import 허용 |
|---|---|
| core/source | 표준 라이브러리, yaml.v3 |
| core/expr | 표준 라이브러리 |
| core/model | source, expr |
| core/matrix | source, expr |
| core/graph | model |
| core/semdiff | source, expr, model, matrix, graph |
| core/report | semdiff, source |

`core/` 아래 어떤 패키지도 `internal/`, `net/http`, `database/sql`, `pgx`, `prometheus`를 import하지 않는다. 이 규칙을 `core/imports_test.go`로 강제한다. 이 테스트는 `go/parser`로 `core/` 아래 모든 비테스트 `.go` 파일의 import를 읽고 위 표와 대조한다.

---

## 3. 공통 규약

- 모듈 경로는 `git remote get-url origin`의 결과에서 도출한다. `https://github.com/<owner>/<repo>.git` 또는 `git@github.com:<owner>/<repo>.git` 형태면 `github.com/<owner>/<repo>`다. 원격이 없으면 작업을 멈추고 사람에게 묻는다.
- `go.mod`의 `go` 지시어는 설치된 Go의 `major.minor.0` 형식으로 쓴다(현재 `go 1.24.0`). 1.23 미만이면 멈추고 보고한다. `major.minor`(`go 1.24`)는 쓸 수 없다. 의존성(`github.com/jackc/pgx/v5` v5.8.0, `golang.org/x/sync`, `golang.org/x/text`)이 `go 1.24.0`을 선언하고, Go 버전 순서에서 `1.24` < `1.24.0`이므로 `go 1.24`이면 빌드가 `go: updates to go.mod needed`로 실패하고 `go mod tidy`가 `1.24.0`으로 되돌린다. Containerfile의 golang 이미지 태그는 `major.minor`(`1.24`)다.
- 로깅은 `log/slog` JSON 핸들러를 쓴다. 레벨은 `QUANTO_LOG_LEVEL`로 정한다.
- 식별자와 코드는 영어로 쓴다. 사용자에게 보이는 출력(PR 코멘트, Check Run, CLI)도 영어로 쓴다.
- 테스트는 표준 `testing`만 쓴다. 테이블 주도 테스트를 기본으로 한다.
- 골든 파일 테스트는 패키지별 `-update` 플래그(`flag.Bool("update", ...)`)로 갱신한다.
- 퍼즈 테스트 대상: `core/source.Load`, `core/expr.ParseTemplate`, `core/matrix.Expand`, `core/semdiff.Compare`, `internal/action`의 워크플로 명령 생성(`FuzzCommand`).

---

## 4. core/source

### 4.1 API

```go
type Position struct {
    File      string
    Line      int
    Column    int
    EndLine   int
    EndColumn int
    Path      string
}
func (p Position) Valid() bool
func (p Position) Contains(line, column int) bool
func (p Position) Cover(q Position) Position
func (p Position) String() string

type Positioned[T any] struct {
    Value T
    Pos   Position
}
func At[T any](v T, pos Position) Positioned[T]

type Kind int
const (
    KindInvalid Kind = iota
    KindScalar
    KindMapping
    KindSequence
)

type Document struct {
    File    string
    Content []byte
}
func Load(file string, content []byte) (*Document, error)
func (d *Document) Root() *Node
func (d *Document) Empty() bool
func (d *Document) LineCount() int

type Node struct{}
func (n *Node) Kind() Kind
func (n *Node) Exists() bool
func (n *Node) Pos() Position
func (n *Node) Path() string
func (n *Node) Tag() string
func (n *Node) IsNull() bool
func (n *Node) Len() int
func (n *Node) Field(name string) *Node
func (n *Node) FieldKey(name string) *Node
func (n *Node) Has(name string) bool
func (n *Node) Fields() []Field
func (n *Node) Keys() []string
func (n *Node) Items() []*Node
func (n *Node) Index(i int) *Node
func (n *Node) Str() (string, bool)
func (n *Node) StrOr(fallback string) string
func (n *Node) PosStr() (Positioned[string], bool)
func (n *Node) Int() (int, bool)
func (n *Node) Bool() (bool, bool)
func (n *Node) StrList() []Positioned[string]
func (n *Node) Walk(fn func(*Node) bool)
func (n *Node) Lookup(path string) *Node
func (n *Node) LineComment() string

type Field struct {
    Name  string
    Key   *Node
    Value *Node
}

type SyntaxError struct {
    File    string
    Line    int
    Message string
    Cause   error
}
func (e *SyntaxError) Error() string
func (e *SyntaxError) Unwrap() error
func (e *SyntaxError) Pos() Position
```

### 4.2 불변식 (실측으로 확인된 사항 포함)

1. **파싱 방식.** `yaml.v3`의 `yaml.Node` 트리를 직접 순회한다. 워크플로를 `map[string]any`나 구조체로 `Unmarshal`하지 않는다. 이 방식에서는 `on` 키가 문자열 `"on"`으로 유지된다(불리언 변환 함정 없음).
2. **컬럼은 rune 기준 1부터 시작한다.** `yaml.v3`가 보고하는 Column은 바이트가 아니라 문자 단위다. 예: `한글키: value`에서 값의 Column은 6이다. 바이트로 계산하지 않는다.
3. **종료 위치 계산.** yaml.v3는 시작 위치만 주므로 직접 계산한다.
   - plain 스칼라 한 줄: `Column + runeLen(Value) - 1`. 이 값이 해당 줄의 후행 공백 제외 폭을 넘으면 여러 줄 plain 스칼라로 보고, 들여쓰기가 첫 연속 줄 들여쓰기 이상이고 새 키(`key:`)나 시퀀스 항목(`- `)이나 주석이 아닌 줄까지 확장한다.
   - 작은따옴표 스칼라: 닫는 `'`까지 스캔한다. `''`는 이스케이프다. 여러 줄에 걸칠 수 있다.
   - 큰따옴표 스칼라: 닫는 `"`까지 스캔한다. `\` 다음 문자는 건너뛴다. 여러 줄에 걸칠 수 있다.
   - 블록 스칼라(`|`, `|-`, `|+`, `>`, `>-`, `>+`): **`Value`의 개행 수를 세지 않는다.** 폴딩(`>`)은 개행을 공백으로 접어서 실제 줄 수보다 작게 나온다. 대신 인디케이터 다음 줄부터 스캔해서, 빈 줄은 건너뛰고, 첫 내용 줄의 들여쓰기 이상인 줄이 이어지는 동안 확장한다. 종료 위치는 마지막 내용 줄의 후행 공백 제외 폭이다.
   - 플로우 컬렉션(`[...]`, `{...}`): 자식의 최대 종료 위치 이후 공백과 쉼표만 지나 만나는 닫는 괄호까지 확장한다. 빈 `[]`, `{}`도 괄호를 포함한다.
   - 블록 매핑·시퀀스: 모든 자식 종료 위치의 최댓값이다.
   - 별칭(`*name`): 토큰 끝까지다.
4. **클램프.** 모든 Position을 문서 범위로 클램프한다. `Line`과 `EndLine`은 `[1, LineCount]`, 컬럼은 `[1, max(1, 해당 줄의 후행 공백 제외 폭)]` 안에 있어야 한다. `EndLine ≥ Line`이고, 같은 줄이면 `EndColumn ≥ Column`이다. 이유: `push:` 같은 null 값 노드는 yaml.v3가 줄 끝을 넘는 컬럼을 보고하고, GitHub Check Run API는 범위 밖 좌표를 거부한다.
5. **줄 인덱스는 CRLF를 LF로 정규화해서 만든다.**
6. **논리 경로.** 루트는 `""`다. 매핑 키는 `부모 + "." + 키`(부모가 빈 문자열이면 키 자체)다. 키가 비었거나 `. [ ] ' "` 공백·탭 중 하나를 포함하면 `부모 + "['" + (작은따옴표를 두 개로 바꾼 키) + "']"`다. 시퀀스 항목은 `부모 + "[i]"`다. 모든 노드에 대해 `root.Lookup(n.Path())`는 같은 Position을 가진 노드를 돌려줘야 한다.
7. **별칭 해석.** 값이 별칭 노드면 대상 노드의 내용을 투명하게 노출한다. 해당 Node의 Position은 별칭 토큰의 위치를 쓴다. `Kind()`는 대상의 Kind를 돌려준다.
8. **머지 키 `<<`.** `Field`, `FieldKey`, `Has`, `Fields`, `Keys`는 `<<`의 값(매핑, 매핑의 별칭, 또는 그런 것들의 시퀀스)에 있는 키를 포함한다. 명시 키가 머지 키보다 우선한다. 시퀀스 안에서는 앞쪽 항목이 우선한다. `<<` 자체는 `Fields`와 `Keys` 결과에 포함하지 않는다. 머지로 들어온 필드의 Position은 앵커 정의 쪽 위치다. 해석 깊이가 64를 넘으면 더 따라가지 않는다. 한 번의 해석 안에서 매핑 노드별 결과를 메모이제이션해서 `<<: [*a, *a]` 중첩이 지수적으로 폭증하지 않게 한다. **GitHub Actions는 앵커와 별칭은 지원하지만 머지 키는 문법 오류로 거부한다.** 머지 키 해석은 YAML 상위 호환을 위해 유지하되, 이 문서의 다른 모든 테스트와 골든 케이스는 머지 키를 쓰지 않고 앵커·별칭만 쓴다.
9. **`Walk`는 별칭을 따라가지 않는다.** 별칭은 리프로 방문한다. 이는 별칭 폭증 입력에 대한 방어다.
10. **빈 문서.** 빈 입력이나 주석만 있는 입력은 에러 없이 `Empty() == true`, `Root() == nil`이다.
11. **nil 안전.** 모든 `*Node` 메서드는 nil 수신자에서 패닉 없이 영값을 돌려준다. `root.Field("a").Field("b").Index(3).Field("c").Exists()`는 중간이 없어도 false다.
12. `StrList`는 스칼라 하나, 시퀀스, 매핑(키 목록) 세 형태를 받는다.
13. `Bool`은 `true/false/yes/no/on/off/y/n`을 대소문자 무시로 인식한다.
14. 구문 오류는 `*SyntaxError`로 반환한다. yaml.v3 메시지 `yaml: line N: msg`에서 줄 번호를 추출한다.
15. `LineComment`는 노드 토큰(별칭이면 별칭 토큰)에 yaml.v3가 붙인 `LineComment`에서 앞의 `#`들과 앞뒤 공백을 제거한 값이다. `key: value # c`에서 주석은 값 노드에 붙는다. nil 수신자와 주석 없음은 빈 문자열이다.

### 4.3 필수 테스트

`textAt(content, pos)` 헬퍼로 Position이 가리키는 실제 텍스트를 추출해 비교한다. 숫자 비교만으로 끝내지 않는다.

- plain, 작은따옴표(`'it''s'`), 큰따옴표(`"say \"hi\""`), 리터럴 블록, strip 리터럴 블록, **폴딩 블록**, 플로우 시퀀스, 플로우 매핑, 빈 플로우 시퀀스, 블록 시퀀스, 중첩 매핑, 인라인 주석 뒤 plain 스칼라의 span
- 한글 값과 한글 키의 span (rune 컬럼 검증)
- 논리 경로 생성, 점을 포함한 키의 대괄호 경로, `Lookup` 왕복
- `on` 키가 문자열로 유지됨
- nil 안전 체인
- `LineComment`: 값 노드의 줄 주석, `#v2`처럼 공백 없는 주석, 주석 없음, nil 수신자
- `StrList` 세 형태와 각 항목의 span
- null 값(`push:`), `Has`로 null 키 존재 확인
- CRLF 입력의 값과 span
- 별칭 해석, 머지 키 해석, 명시 키의 머지 키 우선, 머지 체인 시퀀스
- 구문 오류의 줄 번호
- 빈 문서, 주석 전용 문서
- **불변식 테스트**: 다양한 샘플(매트릭스, 재사용 워크플로, 앵커, 한글, 주석 도배, 깊은 중첩, 플로우 스타일, 빈 값, 긴 표현식, 후행 개행 없음, CRLF)에서 모든 노드가 유효 Position을 갖고, 텍스트 추출이 가능하고, 자식 span이 부모 span 안에 있음을 검증
- **비정상 입력 무패닉 테스트**: 빈 문자열, `[`, `{`, 닫히지 않은 따옴표, 인디케이터만 있는 블록 스칼라, 탭 들여쓰기, 정의 안 된 별칭, `- - - -`, 복합 키, 200단 중첩, 10000자 스칼라, 제어 문자, BOM
- **퍼즈**: 모든 노드에서 `EndLine ≥ Line`, `EndLine ≤ LineCount`, 같은 줄이면 `EndColumn ≥ Column`
- **코퍼스 테스트**: `testdata/corpus/*.yml|*.yaml` 전부를 불변식 검사와 경로 왕복 검사로 돌린다. 디렉터리가 비었거나 없으면 skip한다.

---

## 5. core/expr

범위는 파싱과 참조 추출까지다. 평가기는 만들지 않는다.

### 5.1 문법

- 리터럴: `null`, `true`, `false`, 숫자(JSON 숫자 형식과 `0x` 16진수), 작은따옴표 문자열(`''` 이스케이프)
- 식별자: 컨텍스트 이름과 속성 이름. 영문자로 시작하고 영문자, 숫자, `_`, `-`로 이어진다
- 연산자 우선순위(높은 것부터): `()` 그룹 → `[]` 인덱스, `.` 속성 → `!` → `< <= > >=` → `== !=` → `&&` → `||`
- `.*` 와일드카드 필터
- 함수 호출 `name(arg, ...)`. 함수 이름은 소문자로 정규화한다. 알 수 없는 함수는 파싱 오류가 아니다

### 5.2 API

```go
type Expr interface{ Offset() int }
type Literal struct{ Value any; Off int }
type Ident struct{ Name string; Off int }
type Property struct{ Target Expr; Name string; Off int }
type Index struct{ Target Expr; Index Expr; Off int }
type Star struct{ Target Expr; Off int }
type Unary struct{ Op string; X Expr; Off int }
type Binary struct{ Op string; Left, Right Expr; Off int }
type Call struct{ Name string; Args []Expr; Off int }

type Segment struct {
    Text   string
    Expr   Expr
    IsExpr bool
    Offset int
}
type Template struct{ Segments []Segment }

type Reference struct {
    Context string
    Path    []string
}

type SyntaxError struct {
    Offset  int
    Message string
}

func ParseExpression(s string) (Expr, error)
func ParseTemplate(s string) (*Template, error)
func ParseCondition(s string) (*Template, error)
func References(e Expr) []Reference
func TemplateReferences(t *Template) []Reference
func Functions(e Expr) []string
func IsDynamic(s string) bool
```

- `Offset`은 입력 문자열의 rune 오프셋이다.
- `ParseTemplate`은 `${{ ... }}` 구간을 렉서로 스캔한다. 문자열 리터럴 안의 `}}`는 종료로 보지 않는다. 닫히지 않으면 `*SyntaxError`다.
- `ParseCondition`은 `if:` 의미론이다. 앞뒤 공백을 제거한 문자열 전체가 `${{ ... }}` 하나면 그 안을 식 하나로 파싱한다. `${{`가 없으면 문자열 전체를 식 하나로 파싱한다. 그 외 혼합 형태는 `ParseTemplate`로 처리한다.
- `References`는 컨텍스트 이름과 경로를 소문자로 정규화한다. 리터럴 문자열 인덱스(`a['b']`)는 경로 세그먼트가 되고, 동적 인덱스와 `*`는 `"*"` 세그먼트가 된다. 결과는 정렬하고 중복을 제거한다.
- `IsDynamic(s)`는 `s`에 `${{`가 있으면 true다.

### 5.3 필수 테스트

우선순위 결합(`a || b && c`, `!a == b`), 속성·인덱스 체인, `fromJSON(needs.setup.outputs.matrix)`, `secrets['MY_TOKEN']`, `github.event.pull_request.head.sha`, 와일드카드, 템플릿 혼합(`prefix ${{ a }} mid ${{ b }}`), 문자열 안의 `}}`, 닫히지 않은 템플릿 오류, 조건 세 형태, 대소문자 정규화, 16진수·지수 숫자, 파싱 → 참조 추출 결정성, 퍼즈(패닉 없음).

---

## 6. core/model

### 6.1 API

```go
type Diagnostic struct {
    Code    string
    Message string
    Pos     source.Position
}

var ErrNotWorkflow = errors.New("document is not a workflow mapping")

func Parse(doc *source.Document) (*Workflow, []Diagnostic, error)

type Workflow struct {
    File        string
    Name        string
    Triggers    []Trigger
    Permissions PermissionSet
    Concurrency *Concurrency
    EnvKeys     []string
    SecretRefs   []string
    SecretRefPos map[string]source.Position
    Jobs         []*Job
    JobsPos      source.Position
    FirstKeyPos  source.Position
    Pos          source.Position
}

type Trigger struct {
    Event   string
    Filters map[string][]source.Positioned[string]
    Crons   []source.Positioned[string]
    Inputs  []string
    Pos     source.Position
}

type Job struct {
    ID              string
    Name            source.Positioned[string]
    Needs           []source.Positioned[string]
    If              *Condition
    RunsOn          RunnerSpec
    Strategy        *Strategy
    Permissions     PermissionSet
    Environment     string
    Concurrency     *Concurrency
    TimeoutMinutes  *source.Positioned[string]
    ContinueOnError string
    ContainerImage  string
    Services        []string
    Uses            *ReusableRef
    With            map[string]source.Positioned[string]
    SecretsInherit  bool
    SecretNames     []string
    SecretsPos      source.Position
    Outputs         []string
    Steps           []*Step
    Pos             source.Position
}

type Step struct {
    Index            int
    ID               string
    Name             string
    If               *Condition
    Uses             *ActionRef
    Run              *source.Positioned[string]
    With             map[string]source.Positioned[string]
    EnvKeys          []string
    Shell            string
    WorkingDirectory string
    Pos              source.Position
}

type Condition struct {
    Raw      string
    Template *expr.Template
    ParseErr error
    Pos      source.Position
}

type RunnerSpec struct {
    Labels  []source.Positioned[string]
    Group   string
    Dynamic bool
    Pos     source.Position
}

type Strategy struct {
    Matrix      *source.Node
    FailFast    string
    MaxParallel string
    Pos         source.Position
}

type Concurrency struct {
    Group            string
    CancelInProgress string
    Pos              source.Position
}

type Level int
const (
    LevelNone Level = iota
    LevelRead
    LevelWrite
)

type PermissionSet struct {
    Declared bool
    All      string
    Scopes   map[string]source.Positioned[Level]
    Pos      source.Position
}

type RefKind int
const (
    RefUnknown RefKind = iota
    RefSHA
    RefMutable
)

type ActionRef struct {
    Raw        string
    Owner      string
    Repo       string
    Path       string
    Ref        string
    Kind       RefKind
    Local      bool
    Docker     bool
    DockerImage string
    FirstParty bool
    VersionHint string
    Pos        source.Position
}
func (a *ActionRef) Identity() string

type ReusableRef struct {
    Raw   string
    Local bool
    Owner string
    Repo  string
    Path  string
    Ref   string
    Kind  RefKind
    Pos   source.Position
}
```

`Jobs`는 YAML에 나타난 순서를 유지한다. `Job.SecretsPos`는 잡 `secrets` 값 노드 위치이고, 키가 없으면 영값이다. `JobsPos`는 루트의 `jobs` 키 노드 위치이고, 키가 없으면 영값이다. `FirstKeyPos`는 루트 매핑의 첫 키 노드(`Fields()`의 첫 항목) 위치다. `SecretRefPos`는 `SecretRefs`의 각 이름에서 그 시크릿을 문서 순서상 처음 참조하는 스칼라 값 노드의 위치로 가는 맵이고, 참조가 없으면 nil이다. `Filters`의 키는 다음 집합으로 제한한다: `branches`, `branches-ignore`, `tags`, `tags-ignore`, `paths`, `paths-ignore`, `types`, `workflows`.

### 6.2 정규화 규칙

- **트리거.** `on: push` → 이벤트 하나. `on: [push, pull_request]` → 각각. `on: {event: null | mapping}` → 각각. `types`는 스칼라와 리스트 모두 받는다. `schedule`은 `- cron: '...'` 목록을 `Crons`로 옮긴다. `workflow_dispatch.inputs`와 `workflow_call.inputs`의 키를 정렬해서 `Inputs`에 넣는다.
- **needs.** 스칼라와 리스트 모두 받는다.
- **runs-on.** 스칼라는 라벨 하나, 시퀀스는 라벨 목록, 매핑은 `group`과 `labels`다. 값에 `${{`가 있으면 `Dynamic = true`다.
- **permissions.** `read-all`, `write-all`은 `All`에 넣는다. `{}`는 `Declared = true`에 스코프가 비어 있다는 뜻(전부 none)이다. 매핑은 스코프별 `read`, `write`, `none`이다. 알 수 없는 레벨은 `MODEL-UNKNOWN-PERMISSION-LEVEL` 진단이다. 알 수 없는 스코프 이름은 그대로 보존한다. 블록이 없으면 `Declared = false`다.
- **environment.** 스칼라 또는 `{name, url}`의 `name`이다.
- **concurrency.** 스칼라는 `Group`, 매핑은 `group`과 `cancel-in-progress`다.
- **timeout-minutes, continue-on-error, fail-fast, max-parallel.** 표현식일 수 있으므로 원문 문자열로 보존한다.
- **container.** 스칼라 또는 `{image}`다. **services**는 서비스 이름을 정렬한 목록이다.
- **job `uses`.** `./`로 시작하면 `Local`이다. 아니면 `owner/repo/path@ref`로 분해한다. `secrets: inherit`면 `SecretsInherit = true`이고, 매핑이면 키를 정렬해서 `SecretNames`에 넣는다.
- **step `uses`.**
  - `./` 또는 `.\`로 시작 → `Local`
  - `docker://`로 시작 → `Docker`, `DockerImage`
  - 그 외 `owner/repo[/path]@ref`. `@`가 없으면 `MODEL-ACTION-NO-REF` 진단이고 `Kind = RefUnknown`
  - `Ref`가 대소문자 무관 `^[0-9a-f]{40}$`면 `RefSHA`, 아니면 `RefMutable`. 태그와 브랜치는 정적으로 구분할 수 없으므로 구분하지 않는다
  - `Owner`가 대소문자 무관으로 `actions` 또는 `github`면 `FirstParty`
  - `Identity()`는 `lower(owner/repo)`에 path가 있으면 `/lower(path)`를 붙인 값이다. Local이면 경로, Docker면 `docker://이미지`다
  - `VersionHint`: `uses` 값 노드의 `LineComment()`가 정규식 `^v?[0-9]+(\.[0-9]+)*`로 시작하고 그 뒤가 문자열 끝이거나 공백이면 그 일치 부분이다(예: `# v4.1.1` → `v4.1.1`, `# v4 pinned` → `v4`). 그 외는 빈 문자열이다. 주석은 ref 비교에 쓰지 않는다
- **조건.** `if`는 `expr.ParseCondition`으로 파싱한다. 실패하면 `ParseErr`에 기록하고 `MODEL-EXPR-SYNTAX` 진단을 추가한다. 이 실패로 전체 파싱이 실패하지는 않는다.
- **SecretRefs.** 문서의 모든 스칼라 **값**(키 제외)을 `Walk`로 순회해서, `expr.IsDynamic`이면 `ParseTemplate`로 파싱하고 `Context == "secrets"`인 참조의 첫 경로 세그먼트를 모은다. `*` 세그먼트와 `github_token`은 제외한다. 대문자로 정규화하고 정렬·중복 제거한다. 파싱 실패한 스칼라는 조용히 건너뛴다. 이름마다 처음 만난 스칼라 노드의 위치를 `SecretRefPos`에 기록한다.

### 6.3 진단 코드

`MODEL-NO-ON`, `MODEL-NO-JOBS`, `MODEL-JOB-NOT-MAPPING`, `MODEL-STEP-NOT-MAPPING`, `MODEL-STEP-NO-ACTION`, `MODEL-ACTION-NO-REF`, `MODEL-EXPR-SYNTAX`, `MODEL-UNKNOWN-PERMISSION-LEVEL`. 진단은 파싱을 막지 않는다. 에러는 루트가 매핑이 아니거나 문서가 비었을 때만 `ErrNotWorkflow`로 반환한다.

### 6.4 필수 테스트

트리거 세 형태, 필터와 types 스칼라·리스트, 스케줄, dispatch 입력, needs 두 형태, runs-on 세 형태와 동적 라벨, permissions 네 형태, 액션 참조 전 유형(원격, 경로 포함 원격, SHA 40자, 짧은 SHA는 Mutable, local, docker, @ 없음), FirstParty 판정, 재사용 워크플로 잡과 secrets inherit, 앵커·별칭으로 공유한 runs-on(`runs-on: &runner ubuntu-latest`와 `runs-on: *runner`), 표현식 오류가 있는 if, 코퍼스 전체 파싱 무오류.

---

## 7. core/matrix

### 7.1 API

```go
type Instance struct {
    Values      map[string]any
    FromInclude bool
}

type Diagnostic struct {
    Code    string
    Message string
    Pos     source.Position
}

type Expansion struct {
    Axes          []string
    Instances     []Instance
    Count         int
    Materialized  bool
    Dynamic       bool
    DynamicReason string
    Diagnostics   []Diagnostic
}

const GitHubJobLimit = 256
const MaterializeLimit = 1024

func Expand(matrix *source.Node) (*Expansion, error)
```

`Expand(nil)`는 `Count = 1`, `Instances = [{}]`, `Materialized = true`를 반환한다(매트릭스 없는 잡).

### 7.2 의미론 — GitHub 공식 규칙을 그대로 구현한다

1. **값 변환.** `!!int` → `int64`, `!!float` → `float64`, `!!bool` → `bool`, `!!null` → `nil`, 나머지 스칼라 → `string`, 매핑 → `map[string]any`, 시퀀스 → `[]any`. 값의 동등성은 키를 정렬한 정규 JSON 문자열 비교로 판정한다(타입 엄격). 이 선택을 결정 사항으로 기록한다.
2. **축.** `include`와 `exclude`를 제외한 키가 축이다. 축 순서는 YAML 선언 순서다.
3. **기본 조합.** 축의 데카르트 곱이다. **첫 번째 축이 가장 바깥 루프**다. 예: `version: [10, 12]`, `os: [a, b]` → `{10,a}, {10,b}, {12,a}, {12,b}`. 축이 하나라도 빈 리스트면 기본 조합은 0개이고 `MATRIX-EMPTY-AXIS` 진단이다. **축이 0개면 기본 조합은 0개다**(빈 조합 하나가 아니다). 축도 include도 없으면 `Count = 0`이고 `MATRIX-EMPTY` 진단이다.
4. **exclude.** 기본 조합에만 적용한다. exclude 항목의 모든 키:값이 조합과 일치하면(부분 일치) 그 조합을 제거한다.
5. **include.** exclude 이후 순서대로 처리한다. 각 include 객체에 대해 다음을 수행한다.
   - **병합 후보는 기본 조합(exclude 적용 후)뿐이다.** include가 새로 만든 조합은 이후 include의 병합 후보가 아니다.
   - 객체의 키 중 축 키에 해당하는 것이 모두 그 기본 조합의 값과 같으면(즉 원래 축 값을 하나도 덮어쓰지 않으면) 객체의 키:값 쌍을 그 조합에 전부 추가한다. 이전 include가 추가한 비축 키는 덮어쓴다.
   - 병합된 기본 조합이 하나도 없으면 객체 자체를 새 조합으로 끝에 추가한다(`FromInclude = true`).
   - 중복 제거는 하지 않는다.
   - 참조: GitHub 문서의 설명은 "`{fruit: banana, animal: cat}`은 `{fruit: banana}` 조합에 추가되지 않는다. 그 조합은 원래 매트릭스 조합이 아니기 때문이다"이다. 7.3 골든이 이 규칙을 검증한다.
6. **동적.** 다음 중 하나면 `Dynamic = true`, `Count = 0`, `Materialized = false`이고 사유를 기록한다: 매트릭스 노드 자체가 `${{`를 포함한 스칼라, 축 값 전체가 `${{` 스칼라(예: `os: ${{ fromJSON(...) }}`), `include` 또는 `exclude`가 `${{` 스칼라. 리스트 **원소** 하나가 표현식인 경우(`os: [ubuntu, ${{ vars.X }}]`)는 원소 하나로 세며 동적이 아니다.
7. **크기 제한.** 기본 조합 수(곱)가 `MaterializeLimit`(1024)을 넘으면 실체화하지 않는다. `Count = 곱`(포화 연산: 넘치면 `math.MaxInt`), `Materialized = false`, `Instances = nil`, `MATRIX-TOO-LARGE` 진단이다. include는 반영하지 않는다(그래서 semdiff는 `≥N`으로 표기한다). 상한을 1024로 두는 근거: GitHub은 256개를 넘는 매트릭스를 거부하므로 그 이상을 실체화해도 쓸 데가 없고, 공개 PR 입력이 조합 실체화로 CPU와 메모리를 증폭시키지 못하게 하기 위해서다. `Count > GitHubJobLimit`이면 `MATRIX-OVER-LIMIT` 진단이다.

### 7.3 필수 골든 테스트

**GitHub 문서 예제 (include).**

```yaml
fruit: [apple, pear]
animal: [cat, dog]
include:
  - color: green
  - color: pink
    animal: cat
  - fruit: apple
    shape: circle
  - fruit: banana
  - fruit: banana
    animal: cat
```

기대값은 순서까지 정확히 다음 6개다.

```
{fruit: apple, animal: cat, color: pink, shape: circle}
{fruit: apple, animal: dog, color: green, shape: circle}
{fruit: pear, animal: cat, color: pink}
{fruit: pear, animal: dog, color: green}
{fruit: banana}
{fruit: banana, animal: cat}
```

**exclude 예제.** `os: [macos-latest, windows-latest]`, `version: [12, 14, 16]`, `environment: [staging, production]`, `exclude: [{os: macos-latest, version: 12, environment: production}, {os: windows-latest, version: 16}]` → `Count = 9`.

그 외: 축 없이 include만(각 항목이 조합), 빈 축, 동적 네 형태, 원소 하나만 표현식, 257개 조합의 제한 초과, 곱이 1024인 경우 실체화와 1024를 넘는 경우 비실체화(include 무시, 포화 연산), 객체 값 축, 정수와 문자열 `18` vs `'18'` 구분, 퍼즈.

---

## 8. core/graph

```go
type Edge struct{ From, To string }

type Graph struct {
    Jobs       []string
    Needs      map[string][]string
    Dependents map[string][]string
    Levels     map[string]int
    Cycles     [][]string
    Unresolved []Edge
}

func Build(w *model.Workflow) *Graph
func (g *Graph) Depth() int
func (g *Graph) Width(weights map[string]int) int
func (g *Graph) CriticalPath(durations map[string]time.Duration) ([]string, time.Duration, bool)
```

- `Jobs`는 정렬한다. 존재하지 않는 잡을 가리키는 `needs`는 `Unresolved`에 넣고 간선에서 제외한다.
- 순환은 DFS 3색으로 찾는다. 각 순환은 사전순 최소 잡에서 시작하도록 회전하고, 순환 목록 전체를 정렬한다. 순환에 속한 잡은 `Levels`에서 제외한다.
- 레벨은 의존이 없는 잡이 0이고, 나머지는 `max(의존 잡 레벨) + 1`이다.
- `Depth()`는 `max(Level) + 1`이다. 잡이 없으면 0이다.
- `Width(weights)`는 같은 레벨에 있는 잡 가중치 합의 최댓값이다. 가중치가 없는 잡은 1이다. 합은 포화 덧셈으로 계산한다(넘치면 `math.MaxInt`). 실체화하지 않은 매트릭스는 `math.MaxInt`까지 포화된 수를 가중치로 넘길 수 있어서, 일반 덧셈이면 음수로 넘어가기 때문이다. semdiff는 매트릭스 인스턴스 수를 가중치로 넘긴다.
- `CriticalPath`는 레벨 순서로 가중 최장 경로를 구한다. 경로상 잡 중 하나라도 duration이 없으면 세 번째 반환값이 false다. 동점은 사전순으로 깬다.
- 테스트: 선형, 다이아몬드, 병렬, 미해결 needs, 자기 순환, 다중 순환, 가중 너비, 임계 경로, 결정성.

---

## 9. core/semdiff

### 9.1 API

```go
type Status string
const (
    StatusAdded        Status = "added"
    StatusRemoved      Status = "removed"
    StatusModified     Status = "modified"
    StatusRenamed      Status = "renamed"
    StatusUnanalyzable Status = "unanalyzable"
)

type Significance int
const (
    Low Significance = iota
    Normal
    High
)

type Input struct {
    Path      string
    OldPath   string
    Before    *model.Workflow
    After     *model.Workflow
    BeforeErr error
    AfterErr  error
}

type DurationSource interface {
    JobAverage(workflowPath, jobKey string) (time.Duration, int, bool)
}

type Options struct {
    Durations      DurationSource
    MinSamples     int
}

type Finding struct {
    Kind         string
    Significance Significance
    Subject      string
    Before       string
    After        string
    Detail       string
    Pos          source.Position
    BasePos      source.Position
}

type Metrics struct {
    JobsPerRun     string
    Depth          int
    Width          string
    RunnerMinutes  string
}

type Estimate struct {
    MinutesBefore float64
    MinutesAfter  float64
    Samples       int
}

type FileDiff struct {
    Path     string
    OldPath  string
    Status   Status
    Before   Metrics
    After    Metrics
    Findings []Finding
    Estimate *Estimate
    Error    string
}

func Compare(in Input, opts Options) *FileDiff
func JobKey(j *model.Job) string
func NormalizeRunJobName(name string) (string, bool)
func CronRunsPerDay(expr string) (int, bool, bool)
```

- `MinSamples` 기본값은 5다(0이면 5로 취급).
- **직렬화.** `source.Position`, `semdiff`의 모든 공개 구조체에 snake_case `json` 태그를 붙인다. `Significance`는 `MarshalText`로 `low`, `normal`, `high`를 낸다. 골든 `expected.json`은 `json.MarshalIndent(fileDiff, "", "  ")` 결과에 개행 하나를 붙인 것이다.
- `Before`가 nil이고 `BeforeErr`가 nil이면 추가된 파일이다. `After`도 같은 규칙이다. 한쪽에 에러가 있으면 `StatusUnanalyzable`이고 `Error`에 메시지를 담는다. 그래도 가능한 쪽의 Metrics는 채운다.
- `JobKey`: 잡 `name`이 있고 `${{`가 없으면 그 값, 아니면 잡 ID다. 반환 전에 `NormalizeRunJobName`과 같은 괄호 접미사 제거를 적용한다. 저장 쪽 키와 조회 쪽 키가 같은 규칙을 거치게 하기 위해서다.
- `NormalizeRunJobName`: GitHub 실행 잡 이름에서 끝의 ` (...)` 매트릭스 접미사를 제거한다. ` / `를 포함하면(재사용 워크플로 호출) false를 반환한다.
- `CronRunsPerDay`: 5필드 cron을 받는다. 반환값은 (하루 실행 횟수, 모든 날에 실행되는지, 해석 성공). 분·시 필드는 `*`, `*/n`, `a`, `a-b`, `a-b/n`, 쉼표 목록을 지원한다. 일·월·요일 필드가 모두 `*`면 두 번째 값이 true다. 이름 표기(`MON`, `JAN`)나 형식 오류는 해석 실패다. 패닉하지 않는다.

### 9.2 Metrics

- `JobsPerRun`: 모든 잡의 매트릭스 인스턴스 수 합. 재사용 워크플로를 호출하는 잡(`uses`)은 호출 대상 내부를 볼 수 없으므로 자신의 매트릭스 인스턴스 수만큼 센다(매트릭스 없으면 1). 동적 매트릭스가 하나라도 있으면 `"?"`, 비실체화 상한이면 `"≥N"`. 그 외 10진 정수.
- `Depth`: graph에서 계산한다.
- `Width`: graph에서 계산한 최대 동시 잡 수를 `JobsPerRun`과 같은 규칙의 문자열로 쓴다. 가중치는 인스턴스 수다. 어느 잡이든 동적 매트릭스(또는 해석할 수 없는 매트릭스)가 있으면 `"?"`, 비실체화 상한 매트릭스가 있으면 `"≥N"`, 그 외 10진 정수다. 파일이 부재한 쪽은 빈 문자열이다. report 표에는 이 문자열을 그대로 쓴다(10절 숫자 필드 규칙).
- `RunnerMinutes`: 추정이 가능할 때만 `"%.0f"`, 아니면 빈 문자열.

### 9.3 추정

- **과금 분 기준.** GitHub Actions는 잡마다 사용 시간을 분 단위로 올림해 과금한다. 추정도 이 단위를 따른다.
- 잡별 과금 분은 `JobAverage(path, JobKey(job))`의 평균 실행 시간 `s`(초)에 대해 `ceil(s / 60)`이다. `s ≤ 0`(skipped 등으로 평균 0초)이면 0분이다.
- 추정값은 `Σ(잡의 인스턴스 수 × 잡별 과금 분)`이다. 올림은 잡 단위에서만 하고, 합산 후에는 반올림하지 않는다. 예: 평균 10초 잡의 조합 수 2 → 3은 2 → 3분, 평균 61초 잡 1 → 1은 2 → 2분.
- report의 Metrics 행 라벨은 `Est. billable runner minutes per run`이다.
- 전후 **모든** 잡이 `MinSamples` 이상의 샘플을 갖고, 동적 매트릭스가 없을 때만 계산한다. 하나라도 부족하면 `Estimate = nil`이다. 부분 추정은 하지 않는다.
- 달러로 환산하지 않는다. 분 단위만 쓴다.

### 9.4 Finding 목록 (Kind, 중요도, 영어 문구)

문구의 `{}`는 해당 값으로 치환한다. 코드 식별자는 백틱으로 감싼다. 일반 원칙: 잡 단위 Finding의 `Pos`는 변경을 가장 직접 나타내는 after 노드이고, 그 노드가 없으면 잡 ID 키 노드다.

| Kind | 중요도 | 문구 |
|---|---|---|
| `workflow.added` | Normal | Workflow added |
| `workflow.removed` | Normal | Workflow removed |
| `workflow.renamed` | Low | Workflow renamed from `{before}` |
| `workflow.unanalyzable` | Normal | Could not analyze: {detail} |
| `trigger.added` | Normal | Trigger added: `{subject}` |
| `trigger.removed` | Normal | Trigger removed: `{subject}` |
| `trigger.filter_changed` | Normal | `{subject}` `{detail}` filter: −{before} +{after} |
| `trigger.schedule_changed` | Normal | Schedule: {before} → {after} |
| `trigger.pull_request_target_added` | High | Trigger added: `pull_request_target` (runs with base repository permissions and secrets) |
| `job.added` | Normal | Job added: `{subject}` |
| `job.removed` | Normal | Job removed: `{subject}` |
| `job.renamed` | Low | Job `{before}` renamed to `{after}` |
| `job.runner_changed` | Normal | Job `{subject}` runs-on: {before} → {after} |
| `job.timeout_changed` | Low | Job `{subject}` timeout-minutes: {before} → {after} |
| `job.concurrency_changed` | Low | Job `{subject}` concurrency: {before} → {after} |
| `matrix.count_changed` | 비율 ≥ 2 또는 ≤ 0.5면 High, 아니면 Normal | Job `{subject}` matrix: {before} → {after} jobs |
| `matrix.dynamic` | Normal | Job `{subject}` matrix is computed at runtime; job count unknown |
| `matrix.over_limit` | High | Job `{subject}` matrix expands to {after} jobs (GitHub limit: 256) |
| `graph.depth_changed` | Normal | Longest `needs` chain: {before} → {after} jobs |
| `graph.width_changed` | Normal | Max concurrent jobs: {before} → {after} |
| `graph.cycle` | High | `needs` cycle: {detail} |
| `graph.unresolved` | Normal | Job `{subject}` needs unknown job `{after}` |
| `permissions.broadened` | `{after}`가 `write`면 High, 아니면(`read`) Normal | `{detail}` permission ({subject}): `{before}` → `{after}` |
| `permissions.narrowed` | Low | `{detail}` permission ({subject}): `{before}` → `{after}` |
| `permissions.write_all` | High | `permissions: write-all` set on {subject} |
| `permissions.removed` | High | `permissions` removed from {subject}{detail} |
| `permissions.declared` | Low | `permissions` declared on {subject} |
| `secrets.added` | Normal | New secret referenced: `{subject}` |
| `secrets.inherit_added` | High | Job `{subject}` passes all secrets to `{after}` (`secrets: inherit`) |
| `action.added` | Low | Action added: `{subject}@{after}` |
| `action.removed` | Low | Action removed: `{subject}` |
| `action.third_party_added` | High | New third-party action: `{subject}@{after}`{detail} |
| `action.ref_changed` | Normal | `{subject}`: `{before}` → `{after}` |
| `action.pin_removed` | High | `{subject}` changed from commit SHA to mutable ref `{after}` |
| `estimate.changed` | 변화율 ≥ 50%면 High, 아니면 Normal | Est. billable runner minutes per run: {before} → {after} ({detail} historical runs per job) |

세부 규칙:

- `{subject}`의 스코프 표기는 워크플로 수준이면 `workflow`, 잡 수준이면 `job `+"`id`"다.
- `action.third_party_added`의 `{detail}`은 Mutable이면 ` (mutable ref)`, SHA면 빈 문자열이다. 같은 액션에 대해 `action.added`와 중복 보고하지 않는다(서드파티면 third_party_added만).
- **SHA 표기.** `action.ref_changed`의 `{before}`, `{after}`에서 `Kind`가 `RefSHA`인 ref는 `VersionHint`가 있으면 `<hint> (<sha 앞 7자>)`, 없으면 SHA 앞 7자로 표기한다. 같은 ref에 힌트가 여러 개면 문서 순서상 첫 번째 비어 있지 않은 힌트를 쓴다. 재사용 워크플로 ref는 힌트가 없으므로 SHA 7자다. ref 집합 비교는 원래 ref 문자열로 하므로 주석만 바뀐 경우는 변경이 아니다. 표기 문자열을 정렬해 `, `로 잇는다.
- 액션 비교 단위는 `Identity()`다. 같은 Identity가 여러 스텝에 있으면 ref 집합으로 비교한다. 집합은 정렬해서 `, `로 연결해 표기한다.
- `secrets.added`는 전후 `Workflow.SecretRefs`의 차집합이다.
- 필터 변경은 전체 목록이 아니라 차집합만 보고한다. `Before`는 제거된 원소(전에만 있는 값), `After`는 추가된 원소(후에만 있는 값)를 각각 정렬·중복 제거해 `, `로 연결한 것이다. 문구는 `−` 뒤에 제거 목록, `+` 뒤에 추가 목록을 쓰고, 한쪽이 비면 그 부분(기호 포함)을 생략한다. report는 각 목록을 `, `로 나눠 원소마다 `inline`으로 출력한다. 필터 키 자체가 추가·삭제된 경우도 같은 규칙이다.
- **권한 비교 규칙 (실효 권한 기준).**
  - **실효 권한.** 워크플로 실효 권한은 워크플로 `permissions`가 선언돼 있으면 그 값이고, 없으면 "저장소 기본값"(알 수 없음)이다. 잡 실효 권한은 잡에 `permissions`가 선언돼 있으면 그 값이고, 없으면 워크플로 실효 권한이다.
  - **단위 변화 계산.** 한 단위의 전후 실효 권한 `(B, A)`에서 변화 항목 목록을 만든다. 둘 다 알 수 없음 → 없음. 알려짐 → 알 수 없음 → `permissions.removed`. 알 수 없음 → 알려짐 → `permissions.declared`. 둘 다 알려짐 → 후가 `write-all`이고 전이 아니면 `permissions.write_all` 하나만, 아니면 레벨 순서 `none < read < write`로 스코프별 `permissions.broadened`·`permissions.narrowed`. `read-all`은 모든 스코프가 read, `write-all`은 모든 스코프가 write인 것으로 펼친다. 비교할 스코프 이름 집합은 GitHub 공식 스코프 목록(`actions`, `attestations`, `checks`, `contents`, `deployments`, `discussions`, `id-token`, `issues`, `models`, `packages`, `pages`, `pull-requests`, `repository-projects`, `security-events`, `statuses`)과 전후에 명시된 스코프의 합집합이다. 명시되지 않은 스코프는 none이다.
  - **워크플로 단위.** 워크플로 실효 권한의 변화 항목을 `workflow` 주체로 한 번만 보고한다.
  - **잡 단위 (전후 모두 존재하는 잡, 이름 변경 매칭 포함).** 잡 실효 권한의 변화 항목 중 워크플로 단위 변화 항목과 같지 않은 것만 보고한다. 같음의 기준은 (Kind, 스코프, 전 레벨, 후 레벨)이 모두 같은 것이다. 따라서 전후 모두 상속만 하는 잡은 보고하지 않는다.
  - **broadened 중요도.** `permissions.broadened`의 중요도는 후 레벨로 정한다. 후 레벨이 `write`면 High, `read`면 Normal이다. none → read 확대는 읽기 권한만 부여하므로 단독으로는 코멘트 게시 기준(10절 `Publishable`)을 넘지 않고 Check Run에만 나온다. `write-all` 설정은 기존대로 `permissions.write_all`(High)이다.
  - **추가된 잡.** 추가된 잡이 자체 `permissions`를 선언했을 때만 보고한다. 선언이 `write-all`이면 `permissions.write_all` 하나를, 아니면 선언에서 write인 스코프마다 `permissions.broadened`를 낸다. 이때 `Before`는 `none (new job)`이다. 자체 선언 없이 워크플로 권한을 상속만 하는 추가된 잡은 워크플로 권한이 write 스코프나 `write-all`이어도 내지 않는다. 그 권한은 워크플로 단위 Finding이나 기존 잡과 같은 상속이므로 새 잡이 권한을 넓힌 것이 아니고, 이를 보고하면 잡 추가마다 High Finding이 반복되기 때문이다.
  - **`permissions.removed`의 `{detail}`.** after에서 워크플로와 모든 잡에 `permissions` 선언이 없을 때만 `Detail = "repository-default"`이고 문구 끝에 `; repository default applies`를 붙인다. 그 외에는 `Detail`이 빈 문자열이고 접미 문구가 없다.
  - **위치.** 스코프 항목은 후 실효 권한의 해당 스코프 값 노드(없으면 실효 권한을 정한 `permissions` 노드), `removed`는 `BasePos`에 전 실효 권한의 `permissions` 노드다.
  - **속성.** 전후 실효 권한이 모두 알려진 짝 잡(이름 변경 매칭 포함)에서 어떤 스코프의 레벨이 올라가면, 그 잡 또는 `workflow` 주체에 해당 스코프의 `permissions.broadened`나 `permissions.write_all`이 반드시 있다. `permissions`를 자체 선언한 추가된 잡은 선언에서 write인 스코프마다 같은 조건을 만족한다. 코퍼스 인접 쌍(양방향), 골든 케이스, 퍼즈에서 검증한다. `core/report` 테스트는 ruff#28682 구조(여러 워크플로의 `{}` → `contents: read`, 재사용 호출 잡의 `contents: read` 선언)가 `Publishable` false이고, 같은 구조에서 `contents: write`면 true임을 검증한다.
- **위치.** 각 Finding의 `Pos`는 가장 구체적인 대상 노드다. 매트릭스는 `strategy.matrix` 노드, 권한은 해당 스코프 값 노드(없으면 `permissions` 노드), 액션은 해당 스텝의 `uses` 값 노드, `job.runner_changed`는 after 잡의 `runs-on` 값 노드(after 잡에 `runs-on`이 없으면 잡 ID 키 노드), `job.timeout_changed`는 after 잡의 `timeout-minutes` 값 노드(없으면 잡 ID 키 노드), `job.concurrency_changed`는 after 잡의 `concurrency` 값 노드(없으면 잡 ID 키 노드), `secrets.inherit_added`는 after 잡의 `secrets` 값 노드(`SecretsPos`, 없으면 잡 ID 키 노드), `graph.unresolved`는 after 잡에서 해당 `needs` 항목 값 노드(없으면 잡 ID 키 노드), 그 외 잡 단위는 잡 ID 키 노드, 트리거는 `on` 아래 이벤트 키 노드, 추정과 그 외 그래프 Finding은 워크플로 루트의 `jobs` 키 노드다. `workflow.added`와 after가 파싱된 `workflow.unanalyzable`은 after 문서 루트의 첫 키 노드(`FirstKeyPos`)다. after가 파싱되지 않은 `workflow.unanalyzable`은 영값이다. `workflow.removed`는 `Pos`가 영값이고 `BasePos`가 before 문서 루트의 첫 키 노드다. 어노테이션은 head 파일에만 달 수 있으므로 `workflow.removed`는 어노테이션을 내지 않는다. `secrets.added`는 after에서 그 시크릿을 처음 참조하는 스칼라 노드(`SecretRefPos`)다.
- 스케줄 문구: `'0 * * * *' (24 runs/day)` 형식으로 표기한다. 모든 날 실행이 아니면 `(N runs on matching days)`, 해석 실패면 cron 문자열만 쓴다.
- `graph.width_changed`와 `graph.depth_changed`는 값이 다를 때만 만든다.
- 포맷 변경만 있는 경우(플로우 ↔ 블록, 따옴표, 주석, 키 순서, 앵커 도입)에는 Finding이 **0개**여야 한다.
- **단일 보고 원칙.** 하나의 변경은 하나의 Finding으로 보고한다. 같은 변경을 여러 Kind로 중복 보고하지 않는다.
- **워크플로 추가·삭제.** `workflow.added` 또는 `workflow.removed` 하나만 낸다. 존재하는 쪽의 Metrics만 채운다.
- **트리거.** `pull_request_target`이 추가되면 `trigger.pull_request_target_added`만 내고 `trigger.added`는 내지 않는다. `schedule`은 `trigger.added`·`trigger.removed` 대상에서 제외하고, 추가·삭제·변경 모두 `trigger.schedule_changed` 하나로 낸다. 없는 쪽은 `(none)`이다. cron 목록은 정렬해서 비교·표기한다.
- **매트릭스.** 잡의 인스턴스 수가 256 이하(또는 잡 없음, 동적)에서 256 초과로 새로 넘으면 `matrix.over_limit`만 내고 `matrix.count_changed`는 내지 않는다. 이미 256을 넘던 매트릭스의 수가 바뀌면 `matrix.count_changed`만 낸다. `matrix.dynamic`은 after가 동적이고 before가 동적이 아니거나 잡이 없을 때 낸다. 동적 → 정적 전환은 `matrix.count_changed`(`?` → N, Normal)다. 추가된 잡도 `matrix.dynamic`, `matrix.over_limit` 대상이다.
- **그래프.** 어느 쪽이든 동적 매트릭스가 있으면 `graph.width_changed`를 내지 않는다. 어느 쪽이든 순환이 있으면 `graph.depth_changed`와 `graph.width_changed`를 내지 않는다. 같은 파일에 `matrix.count_changed`, `matrix.over_limit`, `matrix.dynamic` 중 하나라도 있고 잡 ID 집합과 해석된 `needs` 간선 집합이 전후 같으면 `graph.width_changed`를 내지 않는다(너비 변화가 매트릭스 변화에서만 나온 것이므로 중복 보고다). Metrics 표의 `Max concurrent jobs` 행은 그대로 둔다.
- **액션·재사용 워크플로.** 재사용 워크플로를 호출하는 잡의 `uses`도 스텝 액션과 같은 규칙으로 비교한다. Identity는 `lower(owner/repo/path)`, local이면 경로다. 서드파티 판정은 `!Local && !Docker && !FirstParty`이고 docker·local은 `action.added`로 낸다. ref 집합이 바뀐 경우, 전의 ref가 전부 SHA이고 후에 SHA가 아닌 ref가 있으면 `action.pin_removed` 하나만, 그 외는 `action.ref_changed` 하나만 낸다.
- **runs-on 표기.** 라벨을 정렬해 `, `로 연결한다. group이 있으면 앞에 `group <g>: `를 붙인다. 없으면 `(none)`이다. 라벨 순서만 바뀐 경우는 변경이 아니다.
- **추정.** `RunnerMinutes`는 각 쪽에서 독립적으로 추정이 가능하면 채운다. `Estimate`와 `estimate.changed`는 양쪽 모두 가능할 때만 만든다. `Estimate.Samples`는 전후 잡별 샘플 수의 최솟값이다.

### 9.5 잡 이름 변경 탐지

1. before에만 있는 잡 집합 R과 after에만 있는 잡 집합 A를 만든다.
2. 스텝 시그니처는 `uses` Identity 또는 `run` 첫 비공백 줄의 문자열이다. 잡마다 시그니처 멀티셋을 만든다.
3. R × A 모든 쌍의 Jaccard 계수를 계산한다. 0.7 이상인 쌍을 점수 내림차순, 동점이면 (before ID, after ID) 사전순으로 탐욕적으로 1:1 매칭한다.
4. 매칭된 쌍은 `job.renamed` 하나로 보고하고, 이후 모든 잡 수준 비교를 매칭된 쌍에 대해 수행한다. 매칭되지 않은 것만 `job.added`, `job.removed`가 된다.
5. 스텝이 없는 잡(재사용 호출)은 `Uses` 원문이 같을 때만 매칭한다.

### 9.6 정렬

Findings는 (중요도 내림차순, Kind를 위 표 순서로, Subject 사전순, Before, After) 순으로 정렬한다.

### 9.7 위치

`Pos`는 after 문서의 해당 노드 위치다. after에 없는 대상(제거된 것)은 영값이고 `BasePos`에 before 위치를 넣는다.

### 9.7.1 알려진 한계 (의도적으로 다루지 않음)

- 잡 `if`를 평가하지 않는다. 실행되지 않을 잡도 `JobsPerRun`과 최대 동시 잡 수(`Width`)에 포함된다.
- 매트릭스는 조합 수만 비교한다. 축 값을 교체해 실제 커버리지가 줄어도 조합 수가 같으면 Finding이 없다.
- `with:` 입력을 통한 도구 버전 고정 해제(`setup-*` 액션의 `*-version` 값을 정확한 버전에서 `stable`, `latest`, 범위로 바꾸는 것)는 탐지하지 않는다. 액션 ref만 비교한다.

### 9.8 필수 골든 테스트

`testdata/golden/semdiff/<case>/` 아래 `before.yml`, `after.yml`, `expected.json`을 둔다. 파일이 없는 쪽은 `before.yml`이나 `after.yml`을 두지 않는다. 최소 케이스는 다음과 같다.

1. `identical`
2. `reformatted` (플로우 ↔ 블록, 따옴표, 주석, 키 순서, 같은 값을 앵커·별칭으로 바꾸기) → Finding 0
3. `matrix-axis-added` (6 → 24)
4. `matrix-include-docs` (7.3의 문서 예제를 잡 매트릭스로)
5. `matrix-exclude` (12 → 9)
6. `matrix-dynamic` → `matrix.dynamic` 1건만
7. `matrix-over-limit` → `matrix.over_limit` 1건만
8. `permissions-broadened` (contents read → write)
9. `permissions-removed`
10. `permissions-write-all`
11. `third-party-action-mutable`
12. `action-pin-removed` (SHA → 태그) → `action.pin_removed` 1건만
13. `action-major-bump` (v4 → v5)
14. `schedule-added` (`0 * * * *`) → `trigger.schedule_changed` 1건만
15. `pull-request-target-added` → `trigger.pull_request_target_added` 1건만
16. `job-renamed` (스텝 동일) → renamed 1건만
17. `job-added-depth` (needs 추가로 깊이 변화)
18. `needs-cycle` → `graph.cycle` 1건만
19. `secrets-new-and-inherit`
20. `runner-macos-added`
21. `workflow-added`
22. `workflow-removed`
23. `head-unparseable`
24. `anchor-shared-change` (`runs-on: &runner ...`로 정의하고 다른 잡에서 `*runner`로 참조. 앵커 정의 한 줄 변경이 두 잡의 `job.runner_changed` 두 건으로 나와야 한다. 머지 키는 쓰지 않는다)
25. `estimate-with-history` (가짜 DurationSource로 샘플 충분)
26. `estimate-insufficient` (샘플 4개 → 추정 없음)
27. `permissions-job-write-added` (워크플로 `permissions: {}`, 잡에 `contents: write` 추가) → 잡 주체 `permissions.broadened` 1건만
28. `permissions-job-write-removed` (27의 역방향) → 잡 주체 `permissions.narrowed` 1건만
29. `permissions-new-job-write-all` (워크플로 `permissions: {}`, `write-all` 잡 추가) → `permissions.write_all`(`Before = none (new job)`), `job.added`, `graph.width_changed`
30. `permissions-release-split` (cargo-dist 형식 ruff `release.yml` 구조. 워크플로 `contents: write`를 `{}`로 바꾸고 필요한 잡에만 선언) → 워크플로 `contents` narrowed와 `plan` 잡 `contents` narrowed만. 상속만 하는 잡과 같은 레벨을 다시 선언한 잡은 보고하지 않는다
31. `trigger-filter-changed` (검증 보고서 R3: `push`의 `branches`에 하나 추가·`paths`에서 하나 제거, `pull_request`의 `branches`에 하나 추가·`paths-ignore` 신설) → `trigger.filter_changed` 4건, 각 문구는 추가·제거 원소만 담는다
32. `action-sha-version-hint` (SHA 고정 액션의 SHA와 `# v4.1.1` 주석 변경, 주석만 바뀐 액션, 주석 없는 SHA 변경) → `action.ref_changed` 2건. 표기는 `v4.1.1 (b4ffde6)` → `v4.2.2 (11bd719)`, `1234567` → `89abcde`이고 주석만 바뀐 액션은 보고하지 않는다
33. `permissions-new-job-inherits` (워크플로 `contents: write`, 자체 `permissions` 없이 상속만 하는 잡 추가) → 권한 Finding 없음. `job.added`, `graph.depth_changed`만
34. `permissions-broadened-read` (워크플로 `permissions: {}` → `contents: read`, ruff#28682 구조) → `permissions.broadened` 1건(Normal)만. `Publishable`은 false다

**속성 테스트:** 코퍼스의 모든 파일에 대해 `Compare(a, a)`의 Finding이 0개다. 코퍼스의 인접 파일 쌍 `(a, b)`에 대해 다음 대응쌍마다 `Compare(a, b)`의 왼쪽 개수와 `Compare(b, a)`의 오른쪽 개수가 같다: (`trigger.added` + `trigger.pull_request_target_added`, `trigger.removed`), (`job.added`, `job.removed`), (`action.added` + `action.third_party_added`, `action.removed`), (`workflow.added`, `workflow.removed`). `job.renamed` 개수는 양방향이 같다. 퍼즈: 임의 YAML 두 개로 패닉하지 않는다.

---

## 10. core/report

```go
type Meta struct {
    HeadSHA      string
    SkippedFiles int
}

type Annotation struct {
    Path        string
    StartLine   int
    EndLine     int
    StartColumn int
    EndColumn   int
    Level       string
    Title       string
    Message     string
}

const CommentMarker = "<!-- quanto:summary -->"
const MaxBodyRunes = 60000

func Publishable(diffs []*semdiff.FileDiff) bool
func Message(f semdiff.Finding) string
func Markdown(diffs []*semdiff.FileDiff, meta Meta) string
func CheckSummary(diffs []*semdiff.FileDiff, meta Meta) (title, summary string)
func Annotations(diffs []*semdiff.FileDiff) []Annotation
func Text(diffs []*semdiff.FileDiff) string
func JSON(diffs []*semdiff.FileDiff, meta Meta) ([]byte, error)
func NoChanges(headSHA string) string
type DetailsLocation int
const (
    DetailsCheckRun DetailsLocation = iota
    DetailsJobSummary
)
func BelowThreshold(headSHA string, details DetailsLocation) string
func Plain(s string) string
```

- `Publishable`: High Finding이 하나라도 있거나, Kind가 `matrix.`, `graph.`, `estimate.`로 시작하는 Finding이 하나라도 있을 때만 true다. 그 외 Normal Finding만 있는 PR은 코멘트를 만들지 않고 Check Run에만 나온다. 코멘트를 게시할 때 본문(`Markdown`)에는 기존 규칙대로 Normal 이상 Finding이 모두 들어간다.
- `Message`: 9.4 표의 문구를 만든다.
- **사용자 유래 문자열 출력 규칙(마크다운 주입 방지).** YAML 값·키, 잡 ID, 잡 이름, 액션 참조, 파일 경로, cron, 브랜치 필터, runs-on, concurrency, timeout, 에러 메시지, 커밋 SHA 등 PR 내용이나 외부 입력에서 온 문자열은 `core/report`의 비공개 함수 `inline` 하나로만 출력한다. 다른 경로로 사용자 문자열을 출력하는 곳이 없어야 한다. 예외는 펜스 없는 터미널 평문 출력뿐이며, 이 경우는 공개 함수 `Plain`(아래 1~2단계)으로만 출력한다. `inline`은 다음을 순서대로 적용한다.
  1. CR, LF, 탭과 기타 제어 문자(`unicode.IsControl`: C0, DEL, C1. ANSI ESC 포함)를 각각 공백 하나로 바꾼다.
  2. 80 rune을 넘으면 앞 79 rune + `…`로 자른다.
  3. 내부의 가장 긴 연속 백틱보다 하나 더 긴 백틱 펜스로 감싼다. 내용이 백틱으로 시작하거나 끝나면 펜스 안쪽 양쪽에 공백을 하나씩 넣는다. 내용이 비었으면 `` ` ` ``(공백 하나짜리 코드 스팬)를 낸다. 빈 펜스 ```` `` ````는 뒤따르는 코드 스팬과 짝이 어긋나 주입 경로가 되기 때문이다.
- 9.4 표의 `{subject}`, `{before}`, `{after}`, `{detail}` 중 사용자 유래 값은 표에 백틱 표기가 있든 없든 필드 전체를 `inline`으로 출력한다. 스케줄 표기(`'0 * * * *' (24 runs/day)`), runs-on 표기, `(none)`, 순환 경로(`a → b → a`)는 필드 하나로 감싼다. 권한 스코프 표기 `job `+"`id`"는 `job ` + `inline(id)`로 출력한다. `action.third_party_added`의 `{detail}`은 비어 있지 않으면 고정 문구 ` (mutable ref)`를 낸다.
- `trigger.filter_changed`의 `{before}`, `{after}`는 `, `로 나눈 원소 하나하나를 `inline`으로 출력하고 `, `로 잇는다.
- 숫자 필드(매트릭스 수, 그래프 깊이·너비, 추정 분, 샘플 수, Metrics 표의 값)는 `?`, 10진 정수, `≥` + 10진 정수일 때만 그대로 쓰고, 그 외 값은 `inline`으로 출력한다.
- `Markdown`, `CheckSummary`, `Annotations`(message와 title), `Text`, `NoChanges`, `BelowThreshold` 전부 이 규칙을 따른다. `Text`의 파일 경로 줄도 `inline`을 거친다.
- `Plain(s)`: `inline`의 1~2단계만 적용하고 펜스는 씌우지 않는다. `inline`은 `Plain`의 결과에 3단계를 적용하므로 두 출력의 정리 규칙은 같다. 테스트는 `ESC[31m`(색상), `ESC]0;title BEL`(창 제목), `\r` 덮어쓰기, C1 `CSI`, DEL, 80·81 rune 경계를 검증한다.
- `NoChanges(headSHA string) string`은 15.3의 "변화 없음" 코멘트 본문을 만든다: `CommentMarker + "\n## quanto\n\nNo workflow execution changes as of commit " + inline(sha7) + ".\n"`. 분석한 모든 파일의 Finding이 Low 포함 0개일 때만 쓴다.
- `BelowThreshold(headSHA string, details DetailsLocation) string`은 15.3의 "게시 기준 미달" 코멘트 본문을 만든다: `CommentMarker + "\n## quanto\n\nNo changes that meet the comment threshold as of commit " + inline(sha7) + ". " + <상세 위치 문장> + "\n"`. 상세 위치 문장은 호출자가 실행 환경에 맞게 고른다. `DetailsCheckRun`(App, 15.3)은 `Details are in the quanto check run.`, `DetailsJobSummary`(Action, 23.3)는 `Details are in the job summary of the quanto workflow run.`이다. 사용자 문자열을 받지 않고 고정 열거값만 받으므로 상세 위치 문장은 `inline`을 거치지 않는다. Finding이 하나 이상 있지만 `Publishable`이 false일 때 쓴다. 두 문구는 `testdata/golden/report/below-threshold/check-run.md`, `job-summary.md`, `testdata/golden/e2e/comment-below-threshold.md`, `testdata/golden/action/comment-below-threshold.md` 골든으로 고정한다.
- `Markdown`의 형식은 다음과 같다. 파일은 경로 사전순이다. 표는 Metrics 값이 전후로 하나라도 다를 때만 넣는다. 추정 행은 양쪽 값이 있을 때만 넣는다. Low Finding은 코멘트에 넣지 않는다.

```
<!-- quanto:summary -->
## quanto

Execution changes in 1 workflow file.

### `.github/workflows/ci.yml`

| Metric | Before | After |
|---|---:|---:|
| Jobs per run | 7 | 25 |
| Longest `needs` chain | 3 | 3 |
| Max concurrent jobs | 2 | 4 |
| Est. billable runner minutes per run | 43 | 172 |

- Job `test` matrix: 6 → 24 jobs
- `contents` permission (workflow): `read` → `write`
- New third-party action: `peter-evans/create-pull-request@v6` (mutable ref)

---
<sub>Static analysis of workflow files only. No code from this pull request was executed. Commit `abc1234`.</sub>
```

- 파일 수는 단수·복수를 맞춘다(`1 workflow file`, `2 workflow files`).
- 파일이 부재한 쪽(추가·삭제·분석 불가)의 표 셀은 `—`다. 추정 행은 양쪽 값이 모두 있을 때만 넣는다.
- `SkippedFiles > 0`이면 푸터 앞에 `{n} additional workflow files were not analyzed.` 줄을 넣는다.
- 커밋은 `HeadSHA` 앞 7자다.
- **파일 간 중복 합치기.** `Markdown`과 `CheckSummary`에서, 각 렌더링이 보여 주는 Finding(Markdown은 Normal 이상, CheckSummary는 Low 포함) 중 `Message` 결과가 완전히 같은 것이 서로 다른 2개 이상 파일에 나오면, 그 Finding을 파일별 섹션에서 모두 빼고 첫 파일 섹션 앞의 `### Across {N} workflow files` 섹션에 `- {문구} ({k} files)` 한 줄로 모은다. `{N}`은 합쳐진 줄에 관여한 서로 다른 파일 수, `{k}`는 그 문구가 나온 파일 수다. 줄 순서는 파일 경로 사전순·파일 안 Finding 순서로 처음 나타난 순서다. 합친 뒤 남은 Finding도 없고 표도 없는 파일 섹션은 내지 않는다. 머리글의 `Execution changes in {n} workflow files.`는 합치기 전 기준으로 센다. `Annotations`와 `JSON`은 합치지 않고 파일별로 그대로 낸다.
- 본문이 `MaxBodyRunes`를 넘으면 마지막 파일부터 통째로 빼고 `{n} files omitted due to size.` 줄을 넣는다. 파일 중간에서 자르지 않는다. `Across` 섹션은 빼지 않는다.
- `CheckSummary`: Finding이 없으면 title은 `No execution changes`, summary는 분석한 파일 목록이다. 있으면 title은 `{n} execution changes` (Low 포함 전체 개수)이고, summary는 Markdown과 같은 구조에 Low를 포함하고 마커를 뺀 것이다.
- `Annotations`: `Pos.Valid()`인 Finding만 대상이다. Level은 항상 `notice`다. Title은 Kind다(9.4 표에 없는 Kind면 `inline(Kind)`). 시작과 종료가 같은 줄일 때만 컬럼을 채우고, 아니면 컬럼은 0이다.
- `JSON`: 최상위 `{"schema": "quanto.diff/v1", "head_sha": ..., "skipped_files": ..., "files": [...]}`. 필드명은 snake_case다. 들여쓰기 2칸, 끝에 개행 하나.
- `Text`: CLI용 평문이다. 색상 코드 없음. 파일별로 경로 줄, 지표 변화 줄, Finding 목록(Low 포함)을 출력한다.
- 골든 테스트: semdiff 골든 케이스 각각에 대해 `expected.md`와 `expected.txt`를 추가한다.
- 주입 골든 `testdata/golden/report/injection/`: `before.yml`, `after.yml`과 모든 Kind의 필드에 주입 문자열(개행 + `## Approved`, 빈 문자열, `![](...)` 이미지, `@org/admins`, 백틱 3개, `<details>`, `<!-- quanto:summary -->`, 백틱으로 시작·끝나는 값, 링크, 80 rune 초과 값)을 넣은 FileDiff로 `expected.md`, `expected-summary.md`, `expected.txt`, `expected-annotations.json`을 만든다. 테스트는 CommonMark 코드 스팬 규칙으로 스팬 밖 텍스트를 추출해 주입 문자열, 백틱, `<`(`<sub>` 제외), 렌더러가 만들지 않은 제목 줄이 없음을 검증한다.

---

## 11. cmd/quanto (CLI)

`flag` 패키지로 서브커맨드를 구현한다. 종료 코드는 성공 0, 실행 오류 1, 사용법 오류 2다. 버전은 `-ldflags "-X main.version=..."`로 주입하고 기본값은 `dev`다.

| 명령 | 동작 |
|---|---|
| `quanto version` | 버전 출력 |
| `quanto inspect <file> [--format text\|json]` | 트리거, 권한, 잡별 인스턴스 수, runs-on, 액션 목록, 그래프 깊이·너비, 모델 진단 출력 |
| `quanto diff <before> <after> [--format text\|markdown\|json] [--path <name>]` | 두 파일을 비교한다. 경로가 `/dev/null`이거나 빈 파일이면 부재로 취급한다. `--path`의 기본값은 after 경로(없으면 before 경로)다 |
| `quanto serve --role web\|worker\|all` | 페이즈 6에서 추가 |
| `quanto migrate` | 페이즈 6에서 추가 |
| `quanto manifest --webhook-url <url> --homepage-url <url> [--name quanto]` | 페이즈 7에서 추가. GitHub App manifest JSON 출력 |
| `quanto action` | 페이즈 9에서 추가. GitHub Action 진입점(23절). 인자를 받지 않는다 |

`inspect`의 텍스트 출력은 사용자 유래 문자열(파일 경로, 워크플로 이름, 이벤트 이름, 필터 값, cron, 입력 이름, 권한 주체·스코프 이름·`read-all`/`write-all` 값, 잡 ID, runs-on 표기, needs, 잡 `uses`, 액션 Identity와 ref, 진단 메시지)을 값 하나씩 `report.Plain`으로 출력한다. 구조 문구와 계산된 값(인스턴스 수, 레벨, 분류, 고정 진단 코드)은 그대로 쓴다. JSON 출력은 `encoding/json`의 이스케이프에 맡긴다. 테스트는 제어 문자를 넣은 워크플로로 출력에 C0·DEL·C1 rune이 없음을 검증한다.

`inspect`와 `diff`는 입력 파일을 `github.MaxFileSize`(256 KiB) 상한으로 읽는다. `io.LimitReader(file, MaxFileSize+1)`로 읽고, 상한을 넘으면 읽기 실패로 보고 `quanto: read <path>: file exceeds 256 KiB`를 stderr에 쓰고 종료 코드 1을 반환한다. 서버와 CLI가 같은 파일에 같은 결과를 내게 하기 위해서다.

`diff`의 종료 코드: 입력 파일을 읽지 못하거나 상한을 넘으면 1, 사용법 오류면 2, 그 외에는 파싱 실패로 `unanalyzable`이 나와도 0이다(분석 결과를 정상적으로 보고한 것이므로).

CLI 테스트는 `internal/cli`에서 `Run(args []string, stdout, stderr io.Writer) int` 형태로 호출해서 검증한다.

---

## 12. internal/config

| 변수 | 필수 | 기본값 | 용도 |
|---|---|---|---|
| `QUANTO_DATABASE_URL` | serve, migrate | — | PostgreSQL DSN |
| `QUANTO_LISTEN_ADDR` | — | `:8080` | web 역할 주소 |
| `QUANTO_APP_ID` | serve | — | GitHub App ID |
| `QUANTO_PRIVATE_KEY_FILE` | serve | — | App 개인키 PEM 파일 경로 |
| `QUANTO_WEBHOOK_SECRET_FILE` | serve | — | 웹훅 시크릿 파일 경로 |
| `QUANTO_GITHUB_API_URL` | — | `https://api.github.com` | API 기준 URL |
| `QUANTO_WORKER_CONCURRENCY` | — | `4` | 워커 고루틴 수 (1~64) |
| `QUANTO_ALLOW_PRIVATE_REPOS` | — | `false` | 비공개 저장소 분석 허용 |
| `QUANTO_MAX_WORKFLOW_FILES` | — | `50` | PR당 분석 파일 상한 (1~200) |
| `QUANTO_LOG_LEVEL` | — | `info` | debug, info, warn, error |

시작 시 전부 검증하고 실패하면 모든 오류를 모아 한 번에 보고한다. `Config.PoolSize() int32`는 `WorkerConcurrency + 4`를 반환한다(14절). 시크릿 파일은 끝 개행을 제거해서 읽는다. `Config`의 `String()`이나 로그 출력에서 시크릿 값은 `[redacted]`로 표시한다.

---

## 13. internal/github

표준 라이브러리 `net/http`로 구현한다.

- **App JWT (RS256).** 헤더는 `{"alg":"RS256","typ":"JWT"}`, 클레임은 `iat = now - 60s`, `exp = now + 540s`, `iss = App ID 문자열`이다. PEM은 PKCS#1과 PKCS#8 둘 다 받는다. base64url은 패딩 없이 쓴다.
- **설치 토큰.** `POST /app/installations/{id}/access_tokens`. 설치 ID별로 메모리에 캐시하고, 만료 5분 전에 갱신한다. 같은 설치에 대한 동시 갱신은 하나로 합친다(설치 ID별 뮤텍스). 만료된 엔트리(토큰이 없거나 `expires_at`이 지난 것)는 `PruneTokens()`(워커의 1시간 주기 작업) 호출 때만 캐시에서 제거한다. 토큰 갱신 경로에서는 호출하지 않는다. 갱신마다 전역 잠금 아래에서 맵 전체를 순회하면 설치 수에 비례하는 지연이 모든 갱신에 붙기 때문이다. 제거는 맵 잠금 아래에서 엔트리 뮤텍스를 `TryLock`으로만 잡아 사용 중인 엔트리를 건너뛰고, 제거된 엔트리에는 표시를 남겨 그 엔트리를 이미 잡은 호출이 새 엔트리로 다시 시도하게 한다(교착 없음).
- **공통 헤더.** `Accept: application/vnd.github+json`, `X-GitHub-Api-Version: 2022-11-28`, `User-Agent: quanto/<version>`.
- **타임아웃.** `http.Client.Timeout = 30s`이고 모든 호출에 context를 전달한다.
- **페이지네이션.** `Link` 헤더의 `rel="next"`를 따라간다. `per_page=100`이다.
- **에러.** 2xx가 아니면 `*APIError{Status, Message, DocumentationURL}`이다. 403이나 429이면서 `X-RateLimit-Remaining: 0`이거나 `Retry-After`가 있으면 `*RateLimitError{Reset time.Time}`이다(Retry-After 초 우선, 없으면 `X-RateLimit-Reset`).
- **관찰 훅.** `Options.OnResponse func(status int, rateRemaining int)` 콜백으로 메트릭 패키지 의존 없이 관측을 연결한다.

메서드:

```go
func (c *AppClient) App(ctx) (*App, error)
func (c *AppClient) Installation(ctx, installationID int64) (*Client, error)

func (c *Client) PullRequest(ctx, owner, repo string, number int) (*PullRequest, error)
func (c *Client) PullRequestFiles(ctx, owner, repo string, number int) ([]PullRequestFile, error)
func (c *Client) MergeBase(ctx, owner, repo, base, head string) (string, error)
func (c *Client) FileContent(ctx, owner, repo, path, ref string) ([]byte, bool, error)
func (c *Client) CreateCheckRun(ctx, owner, repo string, run CheckRun) (int64, error)
func (c *Client) UpdateCheckRun(ctx, owner, repo string, id int64, run CheckRun) error
func (c *Client) FindCheckRun(ctx, owner, repo, headSHA, name string) (int64, bool, error)
func (c *Client) IssueComments(ctx, owner, repo string, number int) ([]IssueComment, error)
func (c *Client) CreateIssueComment(ctx, owner, repo string, number int, body string) (int64, error)
func (c *Client) UpdateIssueComment(ctx, owner, repo string, id int64, body string) error
func (c *Client) WorkflowRuns(ctx, owner, repo string, limit int) ([]WorkflowRun, error)
func (c *Client) RunJobs(ctx, owner, repo string, runID int64) ([]RunJob, error)
```

- `MergeBase`는 `GET /repos/{o}/{r}/compare/{base}...{head}?per_page=1`의 `merge_base_commit.sha`를 반환한다. 비어 있으면 에러다.
- `PullRequestFiles`는 최대 3000개까지 읽는다. `PullRequestFile{Filename, PreviousFilename, Status}`.
- `FileContent`는 `GET /repos/{o}/{r}/contents/{path}?ref={ref}`에 `Accept: application/vnd.github.raw+json`을 쓴다. 404면 `(nil, false, nil)`이다. 경로 세그먼트는 URL 이스케이프한다. 본문은 `io.LimitReader(body, MaxFileSize+1)`로 읽는다. 256 KiB(`MaxFileSize = 256 << 10`)를 넘으면 `(nil, true, ErrFileTooLarge)`를 반환한다. `ErrFileTooLarge`의 메시지는 `file exceeds 256 KiB`다. 전체 본문을 메모리에 읽은 뒤 크기를 검사하지 않는다.
- Check Run 어노테이션은 요청당 최대 50개다. `CreateCheckRun`은 첫 요청에 50개를 담아 생성하고, 나머지는 같은 Check Run에 `PATCH`로 50개씩 추가한다. 중간 배치가 실패하면 생성된 ID와 에러를 함께 반환한다. `status: completed`, `conclusion: neutral`, `name: quanto`(`CheckRunName`).
- `FindCheckRun`은 `GET /repos/{o}/{r}/commits/{headSHA}/check-runs?check_name={name}&per_page=100`을 페이지네이션으로 읽고, `app.id`가 자기 App ID인 첫 Check Run의 ID를 반환한다. 없으면 `(0, false, nil)`이다.
- `UpdateCheckRun`은 멱등 재개 방식이다. GitHub은 `PATCH`마다 어노테이션을 기존 목록에 덧붙이므로, 먼저 `GET /repos/{o}/{r}/check-runs/{id}`의 `output.annotations_count`(k)를 읽고 `Annotations[k:]`만 50개씩 `PATCH`한다(k는 `[0, len]`로 클램프). 보낼 어노테이션이 없어도 title과 summary를 갱신하는 `PATCH` 한 번을 보낸다. 같은 입력에 대해 어노테이션 순서가 결정적(10절)이라는 전제에 기대며, 재시도 사이에 입력이 바뀌면(예: 이력 통계 갱신) 이미 올라간 앞쪽 어노테이션은 고칠 수 없다.
- `CheckRun{HeadSHA, Title, Summary, Annotations []report.Annotation}`. `internal/github`는 `core/report`를 import해도 된다(반대 방향은 금지).
- `WorkflowRuns`는 `status=completed`로 최신순 한 페이지만 읽는다.
- `RunJobs`는 `filter=latest`다. `RunJob{ID, Name, Conclusion, StartedAt, CompletedAt, Labels}`.
- 테스트는 `httptest.Server`로 모든 메서드, 페이지네이션, 404, 레이트 리밋 두 형태, JWT 서명 검증(테스트에서 생성한 RSA 키로 서명을 검증), 토큰 캐시 재사용과 갱신, 어노테이션 배치를 검증한다.

---

## 14. internal/store

`pgxpool`을 쓴다. `Open(ctx, dsn, maxConns int32)`은 `maxConns > 0`이면 `MaxConns`를 그 값으로 둔다. `serve`와 `migrate`는 `Config.PoolSize()` = `QUANTO_WORKER_CONCURRENCY + 4`를 넘긴다. pgxpool 기본값(`max(4, CPU 수)`)이면 워커 동시성이 그보다 클 때 워커가 연결을 기다리며 처리량이 떨어지기 때문이다. 여유분 4는 유지 작업(reap·prune·큐 깊이), web 핸들러, 마이그레이션 잠금 연결 몫이다. 마이그레이션은 `//go:embed migrations/*.sql`로 포함하고 파일명 순서대로 적용한다. 각 파일은 트랜잭션 하나로 적용하고, `schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT now())`에 기록한다. 적용 전체를 `pg_advisory_lock(7310254)`로 감싸서 여러 인스턴스가 동시에 시작해도 안전하게 만든다. advisory lock은 세션 단위이므로 `pool.Acquire`로 얻은 **단일 연결** 위에서 잠금, 전 파일 적용, 해제를 모두 수행한다.

### 14.1 스키마 (`0001_init.sql`)

```sql
CREATE TABLE installations (
    id BIGINT PRIMARY KEY,
    account_login TEXT NOT NULL,
    account_type TEXT NOT NULL,
    suspended BOOLEAN NOT NULL DEFAULT false,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE repositories (
    id BIGINT PRIMARY KEY,
    installation_id BIGINT NOT NULL REFERENCES installations(id) ON DELETE CASCADE,
    owner TEXT NOT NULL,
    name TEXT NOT NULL,
    private BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX repositories_installation ON repositories (installation_id);

CREATE TABLE webhook_deliveries (
    delivery_id TEXT PRIMARY KEY,
    event TEXT NOT NULL,
    received_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE queue_jobs (
    id BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL,
    payload JSONB NOT NULL,
    dedupe_key TEXT,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'running', 'done', 'dead')),
    attempts INT NOT NULL DEFAULT 0,
    run_after TIMESTAMPTZ NOT NULL DEFAULT now(),
    locked_at TIMESTAMPTZ,
    last_error TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX queue_jobs_ready ON queue_jobs (run_after, id) WHERE status = 'pending';
CREATE UNIQUE INDEX queue_jobs_dedupe ON queue_jobs (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('pending', 'running');

CREATE TABLE analyses (
    id BIGSERIAL PRIMARY KEY,
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    pr_number INT NOT NULL,
    head_sha TEXT NOT NULL,
    base_sha TEXT NOT NULL,
    result JSONB NOT NULL,
    finding_count INT NOT NULL,
    check_run_id BIGINT,
    duration_ms INT NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX analyses_unique ON analyses (repository_id, pr_number, head_sha);

CREATE TABLE pr_comments (
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    pr_number INT NOT NULL,
    comment_id BIGINT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, pr_number)
);

CREATE TABLE job_runs (
    job_id BIGINT PRIMARY KEY,
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    run_id BIGINT NOT NULL,
    workflow_path TEXT NOT NULL,
    job_key TEXT NOT NULL,
    runner_labels TEXT[] NOT NULL,
    conclusion TEXT NOT NULL,
    started_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ NOT NULL,
    duration_seconds INT NOT NULL
);
CREATE INDEX job_runs_lookup ON job_runs (repository_id, workflow_path, job_key, completed_at DESC);

CREATE TABLE job_stats (
    repository_id BIGINT NOT NULL REFERENCES repositories(id) ON DELETE CASCADE,
    workflow_path TEXT NOT NULL,
    job_key TEXT NOT NULL,
    avg_seconds INT NOT NULL,
    p50_seconds INT NOT NULL,
    sample_count INT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (repository_id, workflow_path, job_key)
);
```

`0002_job_runs_completed_at.sql`은 `CREATE INDEX job_runs_completed_at ON job_runs (completed_at);` 한 줄이다. `PruneJobRuns`가 매시간 `completed_at` 조건으로 삭제할 때 전체 스캔을 피하기 위해서다.

`analyses.result`에는 `report.JSON`의 결과만 저장한다. 워크플로 원문은 어떤 테이블에도 저장하지 않는다.

### 14.2 큐 연산

```go
type QueueJob struct {
    ID       int64
    Kind     string
    Payload  []byte
    Attempts int
}

func (s *Store) Enqueue(ctx, kind string, payload any, dedupeKey string) (bool, error)
func (s *Store) Dequeue(ctx) (*QueueJob, error)
func (s *Store) Complete(ctx, id int64, attempts int) error
func (s *Store) Fail(ctx, id int64, attempts int, cause error) error
func (s *Store) Defer(ctx, id int64, attempts int, until time.Time) error
func (s *Store) Kill(ctx, id int64, attempts int, cause error) error
func (s *Store) ReapStale(ctx, olderThan time.Duration) (int64, error)
func (s *Store) PruneQueue(ctx, doneOlderThan, deadOlderThan time.Duration) (int64, error)
func (s *Store) PendingCount(ctx) (int64, error)
```

- `Enqueue`: 중복 키 충돌은 에러가 아니라 `false`다. 빈 `dedupeKey`는 NULL이다. 부분 유니크 인덱스를 쓰므로 `ON CONFLICT (dedupe_key) WHERE dedupe_key IS NOT NULL AND status IN ('pending', 'running') DO NOTHING` 형태로 인덱스 술어를 명시한다.
- `Dequeue`: `status = 'pending' AND run_after <= now()`인 행 하나를 `ORDER BY run_after, id FOR UPDATE SKIP LOCKED`로 잡는다. `status = 'running'`, `locked_at = now()`, `attempts + 1`로 바꾼다. 없으면 `(nil, nil)`이다.
- **소유권 펜싱.** `Complete`, `Fail`, `Defer`, `Kill`은 `Dequeue`가 돌려준 `attempts`를 펜싱 토큰으로 받고 `WHERE id = $1 AND status = 'running' AND attempts = $2` 조건으로만 갱신한다. 갱신된 행이 0이면 `ErrLeaseLost`(`store: job lease lost`)를 반환한다. reap 후 다른 워커가 다시 잡은 작업을 이전 소유자가 덮어쓰지 못하게 하기 위해서다. `ErrNotFound`는 두지 않는다.
- `Fail`: `attempts ≥ 5`면 `dead`, 아니면 `pending`이고 `run_after = now() + min(30s × 2^(attempts-1), 30m)`이다. `last_error`는 500자로 자른다.
- `Defer`: 레이트 리밋용이다. `pending`으로 되돌리고 `run_after = until`, `attempts - 1`로 바꾼다.
- `Kill`: 재시도해도 결과가 같은 실패용이다. attempts와 무관하게 `status = 'dead'`로 바꾸고 `last_error`를 500자로 잘라 기록한다.
- `ReapStale`: `running`이면서 `locked_at`이 기준보다 오래된 행을 `pending`으로 되돌린다. 단 `attempts ≥ 5`인 행은 `dead`로 보내고 `last_error = 'job lease expired'`를 기록한다. 프로세스를 죽이는 작업이 reap과 재실행을 무한 반복하지 않게 하기 위해서다. 반환값은 갱신한 행 수 전체다.
- `PruneQueue`: `status = 'done'`이면서 `updated_at`이 `doneOlderThan`보다 오래된 행과 `status = 'dead'`이면서 `updated_at`이 `deadOlderThan`보다 오래된 행을 삭제하고 삭제한 행 수를 반환한다.

### 14.3 기타 연산

`PruneJobRuns(ctx, olderThan) (int64, error)` (`completed_at`이 기준보다 오래된 `job_runs` 행 삭제. `job_stats`는 다음 수집 때 다시 계산되며 삭제 시점에는 건드리지 않는다), `DeliverySeen(ctx, id) (bool, error)`, `RecordDelivery(ctx, id, event) error` (`ON CONFLICT DO NOTHING`), `PruneDeliveries(ctx, olderThan)`, `UpsertInstallation`, `DeleteInstallation`, `SetInstallationSuspended`, `InstallationSuspended`, `UpsertRepositories`, `RemoveRepositories`, `SaveAnalysis` (`ON CONFLICT (repository_id, pr_number, head_sha) DO UPDATE`), `CommentID`, `SetCommentID`, `InsertJobRuns` (`ON CONFLICT DO NOTHING`), `RecomputeJobStats(ctx, repoID, workflowPath, jobKey)`, `Durations(repoID) semdiff.DurationSource`.

- `RecomputeJobStats`: 해당 키의 `conclusion = 'success'` 최신 30건으로 평균, `percentile_cont(0.5)`, 개수를 계산해 upsert한다.
- `Durations`: 저장소 하나의 `job_stats`를 한 번에 읽어 메모리 맵으로 된 `DurationSource`를 만든다. `JobAverage`는 `avg_seconds`와 `sample_count`를 반환한다.

### 14.4 테스트

`QUANTO_TEST_DATABASE_URL`이 없으면 skip한다. 테스트마다 무작위 이름의 스키마를 만들어 `search_path`로 격리하고 끝나면 삭제한다. 클라우드 세션에서 DB를 띄울 수 없으면, 통합 테스트는 푸시 후 GitHub Actions의 `integration` 잡에서 실행된 결과로 검증한다. 검증 항목: 마이그레이션 멱등성, 동시 마이그레이션(고루틴 두 개), 큐 중복 키, 동시 Dequeue에서 같은 작업을 두 번 잡지 않음(고루틴 8개 × 작업 100개), Fail 백오프와 dead 전이, Kill, Defer의 attempts 복구, ReapStale, reap 후 이전 소유자의 Complete·Fail·Defer·Kill이 `ErrLeaseLost`를 받고 새 실행 상태를 바꾸지 않음, reap 5회 후 dead, PruneQueue, PruneJobRuns(90일 경계)와 `job_runs_completed_at` 인덱스 존재, 딜리버리 중복, 통계 재계산, cascade 삭제.

---

## 15. internal/app

### 15.1 web 역할

| 경로 | 동작 |
|---|---|
| `POST /webhook` | 아래 처리 |
| `GET /healthz` | 항상 200 `ok` |
| `GET /readyz` | DB ping 성공 시 200, 실패 시 503 |
| `GET /metrics` | Prometheus |

`POST /webhook` 처리 순서:

1. `http.MaxBytesReader`로 본문을 25 MiB까지 읽는다.
2. `X-Hub-Signature-256`이 `sha256=` + hex(HMAC-SHA256(secret, body))와 `hmac.Equal`로 일치하지 않으면 401이다. JSON 파싱 전에 검사한다.
3. `X-GitHub-Event`나 `X-GitHub-Delivery`가 없으면 400이다.
4. `DeliverySeen`이 true면 200 `duplicate`다.
5. 이벤트별 처리:
   - `ping` → 200
   - `pull_request`의 `opened`, `synchronize`, `reopened` → 저장소가 비공개이고 허용되지 않았으면 204. 아니면 설치와 저장소를 upsert하고 `analyze_pr`를 enqueue한다. 중복 키는 `pr:{repo_id}:{number}:{head_sha}`. 202.
   - `workflow_run`의 `completed` → 비공개 규칙이 같다. `workflow_run.path`에서 `@` 이후를 잘라낸 값을 `workflow_path`로 쓴다. `ingest_workflow_run`을 enqueue한다. 중복 키는 `run:{run_id}`. 202.
   - `installation`의 `created` → 설치와 저장소 목록을 upsert하고, 허용된 저장소마다 `backfill_repo`를 enqueue한다(중복 키 `backfill:{repo_id}`). `deleted` → 설치를 삭제한다. `suspend`, `unsuspend` → 플래그를 갱신한다. 그 외 액션은 204.
   - `installation_repositories`의 `added`, `removed` → upsert 또는 삭제. 추가된 저장소는 backfill을 enqueue한다.
   - 그 외 이벤트 → 204.
6. 처리가 성공한 뒤에만 `RecordDelivery`를 호출한다. 처리 중 DB 오류는 500이고 딜리버리를 기록하지 않는다(재전송 가능 상태 유지). 동시에 같은 딜리버리가 두 번 처리돼도 upsert와 큐 중복 키 때문에 결과는 같다.
7. 웹 핸들러는 GitHub API를 호출하지 않는다.

서버 설정: `ReadHeaderTimeout 10s`, `ReadTimeout 30s`, `WriteTimeout 30s`, `IdleTimeout 120s`. SIGINT와 SIGTERM에서 20초 그레이스풀 종료. `serve`의 모든 역할은 시작할 때 마이그레이션을 적용한다(advisory lock으로 동시 시작 안전).

### 15.2 worker 역할

- `QUANTO_WORKER_CONCURRENCY`개 고루틴이 `Dequeue`를 반복한다. 비어 있으면 1초 + 0~250ms 지터만큼 쉰다.
- 1분마다 `ReapStale(10m)`, 1시간마다 `PruneDeliveries(7일)`, `PruneQueue(7일, 30일)`, `PruneJobRuns(90일)`, 설치 토큰 캐시 `PruneTokens()`, 15초마다 큐 깊이 게이지를 갱신한다.
- 핸들러는 `recover`로 감싼다. 패닉은 Fail로 기록한다.
- 핸들러 호출마다 `context.WithTimeout(5분)`을 건다. 타임아웃은 Fail이다(`ReapStale`의 10분보다 짧아야 한다). 종료 신호로 인한 취소만 `Defer(now)`다.
- `Complete`·`Fail`·`Defer`·`Kill`이 `store.ErrLeaseLost`를 반환하면 에러가 아니라 `lost lease` 경고 로그만 남기고, `quanto_queue_jobs_total`을 올리지 않는다.
- `*github.RateLimitError`면 `Defer(Reset + 0~30s 지터)`, 페이로드 디코딩 실패처럼 재시도해도 결과가 같은 영구 오류면 `Kill`, 그 외 에러면 `Fail`, 성공이면 `Complete`다.
- 종료 신호를 받으면 새 작업을 잡지 않고, 진행 중인 작업을 최대 60초 기다린다.

### 15.3 `analyze_pr` 핸들러

페이로드: `installation_id`, `repository_id`, `owner`, `repo`, `number`, `head_sha`, `base_sha`.

1. 설치가 정지 상태면 완료 처리하고 끝낸다.
2. 설치 클라이언트를 얻는다.
3. `PullRequestFiles`를 읽는다. 워크플로 파일 판정은 정규식 `^\.github/workflows/[^/]+\.ya?ml$`를 `Filename`과 `PreviousFilename`에 적용한다. 해당 파일이 없으면 Check Run 없이 완료한다.
4. 경로 사전순으로 정렬하고 `QUANTO_MAX_WORKFLOW_FILES`까지만 분석한다. 나머지 개수는 `Meta.SkippedFiles`다.
5. **merge-base.** 워크플로 파일이 하나라도 있으면 `MergeBase(base_sha, head_sha)`로 merge-base를 구한다. PR 파일 목록은 merge-base 기준 diff이므로 before 쪽도 merge-base에서 읽어야 한다. `base_sha`(base 브랜치 끝)에서 읽으면 PR을 연 뒤 base 브랜치가 같은 워크플로를 바꾼 경우 그 변경이 PR의 변경처럼(반대 방향으로) 보고된다. `analyses.base_sha`에도 merge-base를 저장한다. 실패는 일반 에러(Fail)다.
6. 파일마다:
   - `added` → before 없음. `removed` → after 없음. `renamed` → before는 `PreviousFilename`, `OldPath`를 설정.
   - `FileContent`로 before 쪽은 merge-base, head는 `head_sha`에서 읽는다. `github.ErrFileTooLarge`면 해당 쪽 에러는 `ErrFileTooLarge`(`file exceeds 256 KiB`)다.
   - `source.Load` → `model.Parse`. 에러는 Input의 `BeforeErr`, `AfterErr`로 넘긴다.
7. `store.Durations(repository_id)`로 DurationSource를 만든다.
8. 파일별 `semdiff.Compare`.
9. Check Run: `CheckSummary`와 `Annotations`로 만든다. Finding이 없어도 만든다. 먼저 `FindCheckRun(head_sha, "quanto")`로 자기 App의 기존 Check Run을 찾고, 있으면 `UpdateCheckRun`, 없으면 `CreateCheckRun`이다. 재시도와 같은 head의 재분석이 Check Run을 중복 생성하지 않게 하기 위해서다.
   - 수용한 한계: 같은 head를 두 작업이 동시에 분석하면 둘 다 `FindCheckRun`에서 미발견을 보고 `CreateCheckRun`을 호출해 Check Run이 중복될 수 있다. README 알려진 한계에 적는다.
   - 수용한 한계: 재시도 사이에 입력이 바뀌면(이력 통계 갱신 등) `annotations_count` 기준 이어쓰기가 앞쪽 어노테이션과 어긋나 중복·누락이 생길 수 있다. README 알려진 한계에 적는다.
10. 코멘트 전에 `PullRequest`를 다시 읽는다. 현재 head SHA가 페이로드의 `head_sha`와 다르면 코멘트 단계를 건너뛴다.
11. 코멘트 대상 ID는 `pr_comments` 캐시를 먼저 보고, 없으면 `IssueComments` 중 본문이 `CommentMarker`로 시작하고 작성자 login이 `{app slug}[bot]`인 것을 찾는다(App slug는 `App()` 결과를 프로세스 수명 동안 캐시).
    - `Publishable`이면 있으면 수정, 없으면 생성하고 캐시에 기록한다.
    - 아니면서 기존 코멘트가 있으면, 분석한 파일의 Finding이 Low 포함 0개일 때는 `report.NoChanges(head_sha)`, 하나 이상일 때는 `report.BelowThreshold(head_sha, report.DetailsCheckRun)`로 수정한다. 게시 기준 미달 변경이 남은 PR에 "변화 없음"이라고 쓰면 거짓 안심이 되기 때문이다.
    - 아니면서 기존 코멘트가 없으면 아무것도 하지 않는다.
12. `SaveAnalysis`로 저장한다.

### 15.4 `ingest_workflow_run` 핸들러

페이로드: `installation_id`, `repository_id`, `owner`, `repo`, `run_id`, `workflow_path`.

1. `workflow_path`가 `.github/workflows/`로 시작하지 않으면 완료 처리한다.
2. `RunJobs`를 읽는다. `conclusion`이 `success` 또는 `failure`이고 시작·완료 시각이 모두 있으며 `NormalizeRunJobName`이 성공한 잡만 남긴다.
3. `InsertJobRuns`로 저장하고, 영향받은 `(workflow_path, job_key)`마다 `RecomputeJobStats`를 호출한다.

### 15.5 `backfill_repo` 핸들러

`WorkflowRuns(limit 100)`을 읽고 각 run마다 `ingest_workflow_run`을 enqueue한다(중복 키 `run:{run_id}`).

### 15.6 테스트

- 웹: 서명 없음·오류·정상, 필수 헤더 누락, 딜리버리 중복, 이벤트별 enqueue 결과, 비공개 저장소 무시, 본문 크기 초과.
- 워커 핸들러: 가짜 GitHub(`httptest`)와 실제 store(`QUANTO_TEST_DATABASE_URL` 필요)로 검증한다. PR을 연 뒤 base 브랜치가 같은 워크플로를 바꾼 상황에서 base 쪽 변경이 Finding에 나오지 않음(merge-base), 재시도가 Check Run을 새로 만들지 않고 어노테이션을 이어 올림, 핸들러 타임아웃, lost lease(`Complete`·`Fail`·`Defer`·`Kill` 네 경로 각각: 이전 소유자의 결과 기록이 상태·`attempts`·`last_error`·`run_after`·`locked_at`를 바꾸지 않고 `quanto_queue_jobs_total`을 올리지 않음. 같은 입력이 펜싱 없이는 의도한 경로를 타는지 대조 테스트로 확인. `attempts` 5에서 잃은 lease의 `Fail`이 dead로 보내지 않음), 워크플로 변경 없음, 수정·추가·삭제·이름 변경, head 이동 시 코멘트 생략, 기존 코멘트 수정, 변화 없음으로 바뀐 경우의 문구와 게시 기준 미달 변경만 남은 경우의 문구 구분, 어노테이션 51개 이상의 배치, 레이트 리밋 Defer, 이력 수집 필터링과 통계 재계산.
- **종단 테스트** `internal/app/e2e_test.go`: 서명된 `pull_request` 웹훅 → web 핸들러 → 큐 → 워커 한 사이클 → 가짜 GitHub가 받은 Check Run 페이로드와 코멘트 본문을 골든 파일과 비교한다.

---

## 16. internal/metrics

| 이름 | 종류 | 라벨 |
|---|---|---|
| `quanto_webhook_received_total` | counter | `event` |
| `quanto_webhook_rejected_total` | counter | `reason` (`signature`, `headers`, `size`) |
| `quanto_queue_jobs_total` | counter | `kind`, `result` (`done`, `failed`, `deferred`, `dead`) |
| `quanto_queue_depth` | gauge | — |
| `quanto_analysis_duration_seconds` | histogram | — |
| `quanto_findings_total` | counter | `significance` |
| `quanto_github_requests_total` | counter | `status_class` (`2xx`, `4xx`, `5xx`) |
| `quanto_github_rate_limit_remaining` | gauge | — |

전역 기본 레지스트리 대신 `prometheus.NewRegistry()`를 주입한다. Go 런타임 수집기와 프로세스 수집기를 등록한다.

---

## 17. 배포와 문서

- `deploy/.containerignore`: `.git`, `bin`, `testdata/corpus` 세 줄이다. 빌드 컨텍스트에서 이들을 빼서 컨텍스트 크기와 레이어 캐시 무효화를 줄인다. 컨텍스트가 저장소 루트이므로 `make image`가 `podman build --ignorefile deploy/.containerignore`로 지정한다.
- `deploy/Containerfile`: 빌드 스테이지는 `docker.io/library/golang:<go.mod 버전>`, `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=${VERSION}"`. 최종 스테이지는 `gcr.io/distroless/static-debian12:nonroot`. `ENTRYPOINT ["/quanto"]`.
- `deploy/quadlet/`: `quanto.network`, `quanto-db.volume`, `quanto-db.container`(`docker.io/library/postgres:16`, `Secret=quanto-db-password`), `quanto-web.container`(`PublishPort=127.0.0.1:8080:8080`, `Exec=serve --role web`), `quanto-worker.container`(`Exec=serve --role worker`). App 개인키와 웹훅 시크릿은 Podman secret을 파일로 마운트하고 `*_FILE` 변수로 가리킨다. web과 worker는 db 뒤에 시작한다.
- `quanto manifest`: GitHub App manifest JSON을 출력한다. `default_permissions`: `contents: read`, `checks: write`, `pull_requests: write`, `actions: read`, `metadata: read`. `default_events`: `pull_request`, `workflow_run`. `public: true`. `hook_attributes.url`과 `url`은 플래그 값이다. URL 형식을 검증한다.
- `docs/github-app.md`: manifest 흐름으로 App을 만드는 절차, 개인키 다운로드, 설치 방법.
- `docs/deploy.md`: Podman secret 생성 명령, Quadlet 파일 배치 경로(`~/.config/containers/systemd/`), `systemctl --user daemon-reload`와 시작, 리버스 프록시로 TLS를 종단해야 한다는 요구, 로컬 개발 시 웹훅 전달 방법.
- `README.md`: 맨 앞은 GitHub Action 사용법이다(23절): 예시 워크플로, 필요한 권한 표, 포크 PR 동작, 데이터가 GitHub 밖으로 나가지 않는다는 점. 그 뒤에 한 문단 소개, 실제 형식의 예시 코멘트, 요구 권한 표와 각 권한의 용도, 보안 정책(코드 미실행, 원문 미저장), CLI 사용법, "고급" 절로 내린 App 자체 호스팅 링크, 알려진 한계(정적 분석이 잡지 못하는 것, 추정의 전제, 잡 `name`에 표현식을 쓰거나 괄호 접미사가 겹치는 잡은 이력과 매칭되지 않아 추정이 생략된다는 점, 재사용 워크플로 내부는 보지 않는다는 점, 잡 `if`를 평가하지 않으므로 실행되지 않을 잡도 최대 동시 잡 수에 포함된다는 점, 매트릭스 값을 교체해 커버리지가 줄어도 조합 수가 같으면 보고되지 않는다는 점, `with:` 입력을 통한 도구 버전 고정 해제(예: `go-version: '1.22.3'` → `stable`)는 탐지하지 않는다는 점), 라이선스.

---

## 18. CI (`.github/workflows/ci.yml`)

- 트리거: `push`와 `pull_request`. 워크플로 수준 `permissions: contents: read`.
- `test` 잡: `ubuntu-latest`, checkout, `setup-go`(`go-version-file: go.mod`), `test -z "$(gofmt -l .)"`, `go vet ./...`, `go run scripts/check-comments.go`, `go test -race ./...`, `scripts/fetch-corpus.sh`, `go test -run Corpus ./...`, 퍼즈 대상마다 `go test -run '^$' -fuzz <대상> -fuzztime 20s -fuzzminimizetime 5s <패키지>`. 최소화 기본값(60초)이 퍼즈 시간을 잡아먹지 않게 하기 위해서다.
- `integration` 잡: `services.postgres`(`postgres:16`, 헬스체크 포함), `QUANTO_TEST_DATABASE_URL` 설정 후 `go test -race ./internal/...`.
- 액션 참조는 `actions/checkout@v5`, `actions/setup-go@v6`를 쓴다(Node 24 런타임).
- 릴리스 워크플로(`release.yml`)는 23.5를 따른다. `vMAJOR.MINOR.PATCH` 정식 태그에서만 `move-major` 잡이 `vMAJOR` 태그와 릴리스를 갱신하고, `vMAJOR` 태그 push로는 release가 실행되지 않는다.

---

## 19. Makefile

타깃: `fmt-check`, `vet`, `comments`, `test`, `test-race`, `fuzz`, `corpus`, `build`, `image`, `check`(fmt-check, vet, comments, test-race를 차례로). `build`는 `bin/quanto`를 만든다. `.gitignore`에는 `bin/`, `testdata/corpus/*.yml`, `testdata/corpus/*.yaml`, `*.out`, `*.test`만 넣는다. `fuzz` 타깃은 `go test -run '^$' -fuzz <대상> -fuzztime <시간> <패키지>` 형식을 쓴다. 타깃은 그 타깃이 참조하는 대상이 존재하는 페이즈에서 추가한다.

---

## 20. 코퍼스 스크립트

`scripts/fetch-corpus.sh`는 `curl -fsSL --max-time 20`으로 raw.githubusercontent.com에서 워크플로 파일을 받아 `testdata/corpus/`에 저장한다. 실패한 URL은 건너뛰고 `miss`를 출력한다. 마지막에 받은 파일 수를 출력한다. `set -u`를 쓰고 `set -e`는 쓰지 않는다. 대상은 최소 다음을 포함하고, 30개 이상 받는 것을 목표로 목록을 확장한다.

```
rust-lang/rust/master/.github/workflows/ci.yml
nodejs/node/main/.github/workflows/build-tarball.yml
nodejs/node/main/.github/workflows/test-linux.yml
prometheus/prometheus/main/.github/workflows/ci.yml
hashicorp/terraform/main/.github/workflows/checks.yml
actions/checkout/main/.github/workflows/test.yml
ollama/ollama/main/.github/workflows/test.yaml
fastapi/fastapi/master/.github/workflows/test.yml
vercel/next.js/canary/.github/workflows/build_and_test.yml
```

### 20.1 주석 검사

`scripts/check-comments.go`는 첫 줄이 `//go:build ignore`인 독립 프로그램이다. 저장소 루트부터 모든 `.go` 파일(`testdata/` 제외)을 `go/parser.ParseFile(..., parser.ParseComments)`로 읽고, 주석 그룹의 각 주석이 `//go:build`, `//go:embed`, `//go:generate`로 시작하지 않으면 `파일:줄: comment` 형식으로 출력한다. 하나라도 있으면 종료 코드 1이다. 이 파일 자체도 규칙을 지킨다.

---

## 21. 페이즈 보고 형식

페이즈를 끝낼 때 정확히 이 형식으로 보고하고 멈춘다.

```
## Phase N 보고

### 커밋
- 이번 페이즈 커밋 목록 (해시 7자 + 메시지)
- 마지막 커밋이 `origin/main`에 푸시됐는지 (`git status`의 ahead/behind 결과)
- 마지막 푸시 커밋의 GitHub Actions 결과

### 생성·수정한 파일
- path — 한 줄 설명

### 검증
- gofmt: 통과/실패
- go vet: 통과/실패
- go test -race ./...: 통과/실패 (패키지별 결과 요약)
- 추가 검증: 명령과 결과

### 결정 사항
- 명세가 모호했던 지점과 선택한 해석

### 알려진 한계
- 이 페이즈 범위에서 의도적으로 다루지 않은 것

### 골든 파일 갱신
- 갱신한 파일과 근거 (없으면 "없음")
```


---

## 22. 짧은 지시 해석

사용자는 긴 프롬프트 대신 아래 짧은 지시를 쓸 수 있다. 모든 지시는 0절 1번에 따라 최신 `main`에서 시작한다.

### 22.1 `Phase N`

`PHASES.md`의 `## Phase N` 절 코드 블록 내용을 그 페이즈의 작업 지시로 삼아 그대로 수행한다. 코드 블록 안의 지시가 이 문서와 충돌하면 이 문서가 우선한다.

### 22.2 `검수 Phase N` (여러 페이즈면 `검수 Phase 2-3`)

`PHASES.md`의 `## 검수` 절을 수행한다. 이 세션에서는 어떤 파일도 수정하지 않고, 커밋·푸시도 하지 않는다.

### 22.3 `수정 Phase N`

같은 메시지에 붙여 넣은 검수 결과의 치명·중요 항목만 고친다. 항목마다 `phase N: fix <요약>`으로 커밋·푸시한다. 끝나면 `make check` 결과와 항목별 수정 내용을 보고한다.

### 22.4 `재개 Phase N`

`main`의 현재 내용을 Phase N 산출물 목록과 대조해 완료·미완료·불완전 표를 먼저 보여주고 나머지를 수행한다. `phase N: wip` 커밋이 있으면 그 내용을 특히 확인한다. 불완전한 파일은 처음부터 다시 쓰지 않는다.

---

## 23. GitHub Action

quanto는 App 외에 GitHub Action으로도 배포한다. Action은 PR 워크플로 안에서 `GITHUB_TOKEN`으로 같은 분석을 수행하고, 결과를 Job Summary, 워크플로 명령 어노테이션, PR 코멘트로 낸다. DB와 웹훅이 없다. App 코드(`internal/app`, `internal/store`)는 그대로 유지한다.

### 23.1 구조

| 패키지 | 역할 | import 허용 |
|---|---|---|
| `internal/analysis` | PR 파일 목록 → merge-base·head 파일 읽기 → `semdiff.Input` 구성, 비교, 코멘트 본문 선택 | core 전부, `internal/github`(타입) |
| `internal/action` | `quanto action` 서브커맨드 구현 | core 전부, `internal/analysis`, `internal/github` |

- `internal/analysis` API:

```go
const WorkflowDir = ".github/workflows/"

type Source interface {
    PullRequestFiles(ctx, owner, repo string, number int) ([]github.PullRequestFile, error)
    MergeBase(ctx, owner, repo, base, head string) (string, error)
    FileContent(ctx, owner, repo, path, ref string) ([]byte, bool, error)
}
type Request struct {
    Owner, Repo string
    Number      int
    BaseSHA     string
    HeadSHA     string
    MaxFiles    int
}
type Result struct {
    MergeBase string
    Inputs    []semdiff.Input
    Meta      report.Meta
}
func IsWorkflowPath(p string) bool
func Load(ctx, src Source, req Request) (*Result, error)
func (r *Result) Compare(opts semdiff.Options) []*semdiff.FileDiff
func HasFindings(diffs []*semdiff.FileDiff) bool
func CommentBody(diffs []*semdiff.FileDiff, meta report.Meta, details report.DetailsLocation) (body string, publishable bool)
```

- `Load`는 15.3의 3~6단계를 그대로 수행한다. 워크플로 파일이 없으면 `MergeBase`를 호출하지 않고 `Inputs`가 빈 `Result`를 반환한다. GitHub 접근 실패는 에러로 반환하고, 파일 단위 파싱 실패와 `ErrFileTooLarge`는 `Input.BeforeErr`/`AfterErr`로 넘긴다.
- `CommentBody`는 15.3의 11단계 문구 규칙이다: `Publishable`이면 `report.Markdown`, Finding이 있으면 `report.BelowThreshold(head, details)`, 없으면 `report.NoChanges`. App은 `report.DetailsCheckRun`, Action은 `report.DetailsJobSummary`를 넘긴다.
- App의 `analyze_pr`는 `analysis.Load`, `Result.Compare`, `analysis.CommentBody`를 쓴다. App의 동작, 요청 순서, 골든(`testdata/golden/e2e/`)은 바뀌지 않는다.
- `internal/github`에 `NewTokenClient(opts Options, token string) (*Client, error)`를 추가한다. 인증 헤더는 `Authorization: Bearer <token>`이다. 토큰 클라이언트에서 `FindCheckRun`은 App ID가 없으므로 에러를 반환한다. 토큰은 에러 메시지에 넣지 않는다.
- `internal/github`에 `WorkflowFileRuns(ctx, owner, repo, workflowFile string, limit int) ([]WorkflowRun, error)`를 추가한다. `GET /repos/{o}/{r}/actions/workflows/{file}/runs?status=success&per_page={limit}` 한 페이지만 읽는다. `limit`은 `[1, 100]`으로 클램프한다.

### 23.2 입력

| 출처 | 이름 | 처리 |
|---|---|---|
| 환경 변수 | `GITHUB_EVENT_NAME` | `pull_request`, `pull_request_target`가 아니면 안내 문구를 stdout에 쓰고 종료 코드 0 |
| 환경 변수 | `GITHUB_EVENT_PATH` | 이벤트 JSON. 비었거나 읽지 못하거나 `pull_request.number`, `pull_request.head.sha`, `pull_request.base.sha`가 없으면 설정 오류. 25 MiB 상한 |
| 환경 변수 | `GITHUB_TOKEN` | 비면 설정 오류 |
| 환경 변수 | `GITHUB_REPOSITORY` | `owner/repo` 형식이 아니면 설정 오류 |
| 환경 변수 | `GITHUB_API_URL` | 비면 `https://api.github.com`. 절대 http(s) URL이 아니면 설정 오류 |
| 환경 변수 | `GITHUB_STEP_SUMMARY` | 비면 Job Summary를 쓰지 않는다. 쓸 때는 파일 끝에 덧붙인다 |
| action 입력 | `INPUT_COMMENT` | `true`/`false`(대소문자 무시). 비면 `true`. 그 외 값은 설정 오류 |
| action 입력 | `INPUT_ESTIMATE` | 같은 규칙. 비면 `true` |
| action 입력 | `INPUT_MAX_FILES` | 1~200 정수. 비면 50. 그 외 값은 설정 오류 |

검사 순서는 이벤트 이름 → 나머지 설정이다. 설정 오류는 `quanto: <메시지>`를 stderr에 쓰고 종료 코드 1이다. 그 외 모든 경우(분석 실패, API 실패 포함) 종료 코드는 0이다.

### 23.3 동작

1. `analysis.Load`로 분석 입력을 만든다. GitHub API 실패면 `::warning` 명령으로 에러를 쓰고, Job Summary에 `## quanto` + `Analysis failed: a GitHub API request failed. See the step log.`를 쓰고 끝낸다.
2. 워크플로 파일이 없으면 Job Summary에 `## quanto` + `No workflow files changed in this pull request.`를 쓰고 끝낸다. 코멘트는 건드리지 않는다.
3. **추정.** `estimate`가 true면 분석할 파일의 경로 집합(before 쪽은 `OldPath`가 있으면 `OldPath`, 아니면 `Path`. after 쪽은 `Path`)을 정렬하고 앞의 `EstimateMaxFiles`(10)개까지만 조회한다. 경로마다 `WorkflowFileRuns(파일명, 10)`으로 최근 성공 실행 최대 10회를 읽고, 실행마다 `RunJobs`를 읽는다. 잡 필터는 `conclusion == success`, 시작·완료 시각이 있고 완료 ≥ 시작, `NormalizeRunJobName` 성공이다. `(경로, 잡 키)`별로 완료 시각 내림차순·잡 ID 내림차순 최신 30건의 초 단위 평균을 반올림해 `avg`, 개수를 샘플 수로 쓴다(14.3의 `RecomputeJobStats`와 같은 규칙). `WorkflowFileRuns`가 404면 그 경로는 이력 없음이다. 그 외 에러가 하나라도 나면 추정 전체를 생략하고(`Durations = nil`) `::warning`으로 에러를 쓴다. 추정에 쓴 API 호출 수(응답을 받은 요청 수)를 Job Summary에 `Runner-minute estimate: {n} GitHub API calls for {m} workflow files.`(단수·복수 일치)로 기록한다. 생략했으면 `Runner-minute estimate skipped: a GitHub API request failed.`를 쓴다. 상한: 호출 수 ≤ 10 × (1 + 10 × 잡 목록 페이지 수).
4. 파일별 `semdiff.Compare`.
5. **어노테이션.** `report.Annotations` 순서대로 `::notice file=…,line=…,endLine=…[,col=…,endColumn=…],title=…::message`를 stdout에 쓴다. 컬럼은 0이 아닐 때만 넣는다. 워크플로 명령은 전부 단일 함수 `command(name, props, message)`로만 만든다. 이 함수는 메시지에 `%` → `%25`, `\r` → `%0D`, `\n` → `%0A`를, 속성 값에는 추가로 `:` → `%3A`, `,` → `%2C`를 적용한다. stdout에 쓰는 다른 줄은 고정 문구뿐이다.
6. **코멘트.** `comment`가 true일 때만 수행한다. `analysis.CommentBody`로 본문을 정한다. `IssueComments`에서 작성자 login이 `github-actions[bot]`이고 본문이 `CommentMarker`로 시작하는 첫 코멘트를 찾는다. 있으면 수정하고, 없으면 `publishable`일 때만 생성한다. 코멘트 API가 `*APIError` 403을 반환하면(포크 PR의 읽기 전용 토큰 등) 실패로 보지 않고 Job Summary에 `comment skipped: token is read-only` 한 줄을 남긴다. 그 외 에러는 `::warning`으로 쓰고 Job Summary에 `comment skipped: a GitHub API request failed.`를 남긴다. head 재확인과 코멘트 ID 캐시는 하지 않는다.
7. **Job Summary.** `report.CheckSummary`의 summary를 쓰고, 그 뒤에 빈 줄을 사이에 두고 추정 줄과 코멘트 줄을 이 순서로 덧붙인다.

### 23.4 `action.yml` (저장소 루트, composite)

- 메타데이터: `name: quanto workflow diff`, `branding: {icon: activity, color: purple}`(Marketplace 표시용).
- **표시 이름과 제품명.** Marketplace 표시 이름은 `action.yml`의 `name`("quanto workflow diff")이다. 제품명, CLI(`quanto`), 바이너리 이름, 저장소 이름, Go 모듈 경로, PR 코멘트 제목(`## quanto`), 코멘트 마커, `uses` 예시(`BETAER-08/quanto@v1`)는 quanto로 유지한다. 근거: Marketplace는 기존 GitHub 사용자·조직명 또는 기존 Action과 같은 `name`을 거부한다.
- 입력: `github-token`(기본 `${{ github.token }}`), `comment`(기본 `true`), `estimate`(기본 `true`), `max-files`(기본 `50`), `version`(기본 빈 문자열 = action ref와 같은 태그).
- 설치 단계(bash): `RUNNER_OS`/`RUNNER_ARCH`를 `quanto-linux-amd64`, `quanto-linux-arm64`, `quanto-darwin-amd64`, `quanto-darwin-arm64`, `quanto-windows-amd64.exe` 중 하나로 매핑한다. 그 외 조합은 `::error` 후 종료 코드 1.
  - 저장소와 ref: `github.action_repository`, `version` 입력 또는 `github.action_ref`. 비어 있으면 `GITHUB_ACTION_PATH`의 `_actions/<owner>/<repo>/<ref>`에서 도출한다.
  - `GITHUB_ACTION_PATH`에 `/_actions/`가 없고(`uses: ./` 로컬 액션) `version`이 비었으면 릴리스를 받지 않고 action 디렉터리에서 `go build`로 만든다(Go가 설치돼 있어야 한다).
  - 그 외에는 `https://github.com/<repo>/releases/download/<ref>/`에서 바이너리와 `checksums.txt`를 받고(받지 못하면 `::error` 후 종료 코드 1), 해당 파일의 sha256을 `checksums.txt`와 대조한다. 항목이 없거나 불일치하면 실행하지 않고 `::error` 후 종료 코드 1.
- 실행 단계: `GITHUB_TOKEN`, `INPUT_COMMENT`, `INPUT_ESTIMATE`, `INPUT_MAX_FILES`를 env로 넘기고 `quanto action`을 실행한다. 입력 값을 `run:` 본문에 `${{ }}`로 직접 넣지 않는다.

### 23.5 릴리스와 도그푸딩

- `.github/workflows/release.yml`: `v*` 태그 푸시에서 테스트 후 23.4의 5개 조합을 `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=<tag>"`로 만들고, `sha256sum` 형식의 `checksums.txt`를 만든 뒤 `actions/attest-build-provenance`로 바이너리에 빌드 출처 증명을 붙이고 `gh`로 Release를 만든다. 같은 태그의 Release가 이미 있으면 자산을 `--clobber`로 교체한다. 액션은 커밋 SHA로 고정한다. 워크플로 수준 `permissions: contents: read`, `release` 잡 수준 `contents: write`, `id-token: write`, `attestations: write`.
  - **트리거 필터.** `on.push.tags`는 `v[0-9]+.[0-9]+.[0-9]+*`다. `vMAJOR` 태그(`v1`) push는 필터에 걸리지 않으므로 메이저 태그 이동이 release 워크플로를 다시 트리거해 중복 빌드하지 않는다(`GITHUB_TOKEN` push가 워크플로를 트리거하지 않는 것과 별개의 이중 방어).
  - **메이저 태그 자동 이동.** `release` 잡의 `classify` 단계가 태그를 `^(v[0-9]+)\.[0-9]+\.[0-9]+$`로 판정해 일치할 때만 `major` 출력을 낸다. `-rc.1` 같은 접미사가 붙은 prerelease 태그는 출력이 비어 이동하지 않는다. `move-major` 잡은 `needs: release`, `if: needs.release.outputs.major != ''`이고 권한은 `contents: write` 하나다. `git tag -f vMAJOR $GITHUB_SHA` 후 `git push -f origin refs/tags/vMAJOR`로 태그를 옮기고, `gh release download`로 방금 만든 릴리스의 바이너리와 `checksums.txt`를 받아 `sha256sum -c`로 검증한 뒤 `vMAJOR` 릴리스에 `--clobber`로 올린다. `vMAJOR` 릴리스가 없으면 `--latest=false`로 만든다.
  - `vX.Y.Z` 태그 생성은 사람이 한다. 에이전트는 태그를 만들거나 옮기지 않는다(0절). 메이저 태그 이동은 이 워크플로만 한다.
- `.github/workflows/quanto.yml`: `pull_request`(`paths: ['.github/workflows/**']`), `permissions: contents: read, pull-requests: write, actions: read`, checkout → setup-go(`go-version-file: go.mod`) → `uses: ./`.

### 23.6 테스트

- `internal/analysis`: 계획(`planFiles`), 워크플로 경로 판정, merge-base·head 읽기, 파일 상한, 에러 전파, `CommentBody` 세 경우.
- `internal/action`: `httptest` 가짜 GitHub로 정상, 포크 PR 403, 이벤트 불일치, 설정 오류, 추정 실패 폴백, 기존 코멘트 갱신, 사람 코멘트 미수정, 게시 기준 미달 시 코멘트 미생성. e2e 골든 `testdata/golden/action/`: `summary.md`(Job Summary), `commands.txt`(stdout 워크플로 명령), `comment.md`(코멘트 본문). 픽스처는 semdiff 골든 `matrix-axis-added`.
- 워크플로 명령: 개행, `::set-output`, `::add-mask::`, `::stop-commands::` 주입 입력이 한 줄 명령 안에 갇히는지, 속성 구분자가 이스케이프되는지 검증한다. 퍼즈 `FuzzCommand`: 임의 속성·메시지에서 출력이 개행 없는 한 줄이고, 역이스케이프하면 원래 값이 나온다.
- 퍼즈 대상에 `internal/action.FuzzCommand`를 추가한다(3절, 18절, 19절).
