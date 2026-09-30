package source

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var invariantSamples = map[string]string{
	"matrix": `name: CI
on:
  push:
    branches: [main]
  pull_request:
jobs:
  test:
    runs-on: ${{ matrix.os }}
    strategy:
      fail-fast: false
      matrix:
        os: [ubuntu-latest, windows-latest, macos-latest]
        node: [18, 20, 22]
        include:
          - os: ubuntu-latest
            node: 23
            experimental: true
        exclude:
          - os: windows-latest
            node: 18
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-node@v4
        with:
          node-version: ${{ matrix.node }}
      - run: npm test
`,
	"reusable": `on:
  workflow_call:
    inputs:
      target:
        required: true
        type: string
    secrets:
      token:
        required: false
jobs:
  call:
    uses: org/repo/.github/workflows/build.yml@v1
    with:
      target: ${{ inputs.target }}
    secrets: inherit
  local:
    uses: ./.github/workflows/local.yml
    secrets:
      token: ${{ secrets.TOKEN }}
`,
	"anchors": `x-defaults: &defaults
  runs-on: ubuntu-latest
  timeout-minutes: 10
jobs:
  a:
    <<: *defaults
    steps: &steps
      - run: echo a
  b:
    <<: *defaults
    runs-on: macos-latest
    steps: *steps
  c:
    <<: [*defaults]
    env: {A: 1}
`,
	"korean": `name: 빌드와 테스트
on: push
jobs:
  빌드:
    name: 한글 잡 이름 🚀
    runs-on: ubuntu-latest
    steps:
      - name: 설명
        run: echo "안녕하세요 세계"
      - run: |
          echo 첫째 줄
          echo 둘째 줄
`,
	"comments": `# header comment
# another
on: # trailing
  push: # push trigger
    branches: # list
      - main # main branch
      # interleaved
      - 'release/**' # quoted
# middle
jobs: # jobs
  a: # job a
    runs-on: ubuntu-latest # runner
    steps:
      # before step
      - run: echo hi # inline
# footer
`,
	"deep": `a:
  b:
    c:
      d:
        e:
          f:
            g:
              h:
                - i:
                    j:
                      - - k
                        - l
                      - m: [n, {o: p}]
`,
	"flow": `on: {push: {branches: [main, dev]}, pull_request: {types: [opened, synchronize]}}
jobs: {a: {runs-on: ubuntu-latest, steps: [{run: echo}, {uses: 'actions/checkout@v4'}]}, b: {needs: [a], runs-on: [self-hosted, linux]}}
empty: {x: [], y: {}, z: [ ], w: { }}
multi: [
  one,
  two, # comment
  {three: 3}
]
`,
	"empty values": `on:
  push:
  workflow_dispatch:
jobs:
  a:
    runs-on:
    env: {}
    steps: []
    with: ~
    other: null
    q: ''
    dq: ""
`,
	"long expressions": `jobs:
  a:
    if: ${{ github.event_name == 'push' && (startsWith(github.ref, 'refs/tags/') || contains(github.event.head_commit.message, '[release]')) && !cancelled() }}
    runs-on: ${{ fromJSON(needs.setup.outputs.runners)[matrix.index] }}
    env:
      LONG: "${{ secrets.A }}-${{ secrets.B }}-${{ vars.C }}-${{ github.run_id }}-${{ github.run_attempt }}"
      FOLDED: >-
        ${{ github.event.pull_request.head.sha
        || github.sha }}
`,
	"no trailing newline": "on: push\njobs:\n  a:\n    runs-on: ubuntu-latest\n    steps:\n      - run: echo end",
	"crlf":                "on:\r\n  push:\r\njobs:\r\n  a:\r\n    runs-on: ubuntu-latest\r\n    steps:\r\n      - run: |\r\n          echo a\r\n          echo b\r\n      - uses: 'actions/checkout@v4'\r\n",
	"multiline plain": `a: this is a long
  plain scalar that continues
  over lines
b:
  - item that
    wraps
  - next
c: "double
  quoted"
d: 'single
  quoted'
`,
	"tags and anchors": `a: !!str 123
b: &anchor !!int 5
c: *anchor
d: &m
  k: v
e: !custom
  x: 1
f: &q !!str
  wrapped
`,
	"block scalars": `a: |
  x

  y
b: |+
  keep

c: >2
    indented
d: |-
e: >
  last
`,
}

func TestInvariants(t *testing.T) {
	names := make([]string, 0, len(invariantSamples))
	for n := range invariantSamples {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			if n := checkInvariants(t, name, invariantSamples[name]); n == 0 {
				t.Fatalf("no nodes visited")
			}
		})
	}
}

func deepNesting(depth int) string {
	var b strings.Builder
	for i := 0; i < depth; i++ {
		b.WriteString(strings.Repeat(" ", i))
		b.WriteString("k:\n")
	}
	b.WriteString(strings.Repeat(" ", depth))
	b.WriteString("v: end\n")
	return b.String()
}

func TestMalformedInputsDoNotPanic(t *testing.T) {
	inputs := map[string]string{
		"empty":             "",
		"open bracket":      "[",
		"open brace":        "{",
		"unclosed single":   "a: 'unclosed\n",
		"unclosed double":   "a: \"unclosed\n",
		"indicator only":    "a: |\n",
		"folded only":       "a: >-",
		"tab indentation":   "a:\n\tb: c\n",
		"undefined alias":   "a: *nope\n",
		"nested dashes":     "- - - -\n",
		"complex key":       "? [a, b]\n: value\n? {c: d}\n: other\n",
		"deep nesting":      deepNesting(200),
		"deep flow":         strings.Repeat("[", 200) + strings.Repeat("]", 200),
		"long scalar":       "a: " + strings.Repeat("x", 10000) + "\n",
		"control chars":     "a: b\x01c\n",
		"bom":               "\ufeffa: b\n",
		"bom only":          "\ufeff",
		"only dashes":       "---\n",
		"multi documents":   "a: 1\n---\nb: 2\n",
		"colon only":        ":\n",
		"alias of alias":    "a: &a x\nb: &b *a\nc: *b\n",
		"merge scalar":      "a: &a x\nb:\n  <<: *a\n",
		"merge null":        "b:\n  <<:\n  c: d\n",
		"trailing spaces":   "a:    \nb: c   \n",
		"windows line only": "\r\n\r\n",
		"lone cr":           "a: b\rc: d\r",
		"key without value": "a\n",
		"null document":     "~\n",
	}
	names := make([]string, 0, len(inputs))
	for n := range inputs {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		t.Run(name, func(t *testing.T) {
			content := inputs[name]
			d, err := Load(name, []byte(content))
			if err != nil {
				return
			}
			exercise(t, d, content)
		})
	}
}

func exercise(t *testing.T, d *Document, content string) {
	t.Helper()
	root := d.Root()
	root.Walk(func(n *Node) bool {
		pos := n.Pos()
		checkBounds(t, n.Path(), d, content, pos)
		_ = root.Lookup(n.Path())
		n.Kind()
		n.Tag()
		n.IsNull()
		n.Len()
		n.Str()
		n.Int()
		n.Bool()
		n.StrList()
		n.Items()
		for _, f := range n.Fields() {
			checkBounds(t, n.Path()+" key", d, content, f.Key.Pos())
			f.Value.Pos()
		}
		return true
	})
}

func FuzzLoad(f *testing.F) {
	for _, s := range invariantSamples {
		f.Add([]byte(s))
	}
	f.Add([]byte("a: [1, {b: c}]\n"))
	f.Add([]byte("a: |\n  x\nb: 'y'\n"))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Load("fuzz.yml", data)
		if err != nil || d.Empty() {
			return
		}
		lc := d.LineCount()
		check := func(p Position) {
			if p.EndLine < p.Line || p.EndLine > lc || p.Line < 1 {
				t.Fatalf("line range %v outside 1..%d", p, lc)
			}
			if p.EndLine == p.Line && p.EndColumn < p.Column {
				t.Fatalf("end column before start %v", p)
			}
		}
		d.Root().Walk(func(n *Node) bool {
			check(n.Pos())
			for _, fld := range n.Fields() {
				check(fld.Key.Pos())
				check(fld.Value.Pos())
			}
			return true
		})
	})
}

func corpusFiles(t *testing.T) []string {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("read corpus: %v", err)
	}
	var files []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		files = append(files, filepath.Join(dir, name))
	}
	sort.Strings(files)
	return files
}

func TestCorpus(t *testing.T) {
	files := corpusFiles(t)
	if len(files) == 0 {
		t.Skip("testdata/corpus is empty; run scripts/fetch-corpus.sh")
	}
	total := 0
	for _, file := range files {
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		n := checkInvariants(t, filepath.Base(file), string(content))
		total += n
		t.Logf("%s: %d nodes", filepath.Base(file), n)
	}
	t.Logf("corpus files: %d, nodes: %d", len(files), total)
}
