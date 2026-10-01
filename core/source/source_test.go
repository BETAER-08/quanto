package source

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestScalarAndCollectionSpans(t *testing.T) {
	cases := []struct {
		name    string
		content string
		path    string
		want    string
	}{
		{"plain", "a: hello\n", "a", "hello"},
		{"single quoted", "a: 'it''s'\nb: 1\n", "a", "'it''s'"},
		{"double quoted", "a: \"say \\\"hi\\\"\"\nb: 1\n", "a", `"say \"hi\""`},
		{"double quoted multiline", "a: \"one\n  two\"\nb: 1\n", "a", "\"one\n  two\""},
		{"literal block", "a: |\n  line1\n  line2\nb: x\n", "a", "|\n  line1\n  line2"},
		{"literal block with blank line", "a: |\n  line1\n\n  line2\n\nb: x\n", "a", "|\n  line1\n\n  line2"},
		{"strip literal block", "a: |-\n  keep\n  this\nb: x\n", "a", "|-\n  keep\n  this"},
		{"folded block", "a: >\n  one\n  two\n\n  three\nb: x\n", "a", ">\n  one\n  two\n\n  three"},
		{"folded strip block", "a: >-\n  one\n  two\nb: x\n", "a", ">-\n  one\n  two"},
		{"block in sequence", "steps:\n  - run: |\n      echo a\n      echo b\n  - run: x\n", "steps[0].run", "|\n      echo a\n      echo b"},
		{"flow sequence", "a: [x, 'y', \"z\"]\n", "a", "[x, 'y', \"z\"]"},
		{"flow mapping", "a: {k: v, n: {}}\n", "a", "{k: v, n: {}}"},
		{"nested flow", "a: [[b], {c: d}]\n", "a", "[[b], {c: d}]"},
		{"empty flow sequence", "a: []\nb: 1\n", "a", "[]"},
		{"empty flow mapping", "a: {}\n", "a", "{}"},
		{"multiline flow sequence", "a: [\n  x,\n  y\n]\nb: 1\n", "a", "[\n  x,\n  y\n]"},
		{"block sequence", "a:\n  - x\n  - y\nb: 1\n", "a", "- x\n  - y"},
		{"nested mapping", "a:\n  b:\n    c: 1\n  d: 2\ne: 3\n", "a", "b:\n    c: 1\n  d: 2"},
		{"plain after inline comment", "a: foo # comment here\nb: 1\n", "a", "foo"},
		{"multiline plain", "a: plain\n  continued here\nb: 1\n", "a", "plain\n  continued here"},
		{"tagged scalar", "a: !!str 12\n", "a", "!!str 12"},
		{"anchored scalar", "a: &x value\n", "a", "&x value"},
		{"alias token", "a: &x value\nb: *x\n", "b", "*x"},
		{"expression", "if: ${{ github.event_name == 'push' }}\n", "if", "${{ github.event_name == 'push' }}"},
		{"no trailing newline", "a: b", "a", "b"},
		{"root mapping", "a: 1\nb: [2]\n", "", "a: 1\nb: [2]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := mustLoad(t, tc.content)
			n := mustLookup(t, d, tc.path)
			if got := textAt(tc.content, n.Pos()); got != tc.want {
				t.Fatalf("text = %q, want %q (pos %v)", got, tc.want, n.Pos())
			}
		})
	}
}

func TestKeySpans(t *testing.T) {
	content := "jobs:\n  'quoted key': 1\n  plain-key: 2\n"
	d := mustLoad(t, content)
	jobs := mustLookup(t, d, "jobs")
	cases := map[string]string{"quoted key": "'quoted key'", "plain-key": "plain-key"}
	for name, want := range cases {
		k := jobs.FieldKey(name)
		if k == nil {
			t.Fatalf("FieldKey(%q) nil", name)
		}
		if got := textAt(content, k.Pos()); got != want {
			t.Errorf("key %q text = %q, want %q", name, got, want)
		}
	}
}

func TestKoreanRuneColumns(t *testing.T) {
	content := "한글키: 한글 값\nname: 빌드 🚀 test\n"
	d := mustLoad(t, content)
	root := d.Root()
	v := root.Field("한글키")
	pos := v.Pos()
	if pos.Line != 1 || pos.Column != 6 || pos.EndLine != 1 || pos.EndColumn != 9 {
		t.Fatalf("value position = %v, want 1:6-1:9", pos)
	}
	byteColumn := strings.Index(content, "한글 값") + 1
	if byteColumn == pos.Column {
		t.Fatalf("byte column %d equals rune column; test cannot detect byte-based columns", byteColumn)
	}
	if got := textAt(content, pos); got != "한글 값" {
		t.Fatalf("value text = %q", got)
	}
	k := root.FieldKey("한글키")
	if kp := k.Pos(); kp.Column != 1 || kp.EndColumn != 3 || textAt(content, kp) != "한글키" {
		t.Fatalf("key position = %v text %q", kp, textAt(content, kp))
	}
	n := root.Field("name")
	if got := textAt(content, n.Pos()); got != "빌드 🚀 test" {
		t.Fatalf("emoji value text = %q (pos %v)", got, n.Pos())
	}
	if n.Pos().EndColumn != 15 {
		t.Fatalf("emoji value end column = %d, want 15", n.Pos().EndColumn)
	}
}

func TestPathsAndLookup(t *testing.T) {
	content := strings.Join([]string{
		"jobs:",
		"  build:",
		"    steps:",
		"      - uses: actions/checkout@v4",
		"      - run: make",
		"'a.b':",
		"  c: 1",
		"\"it's\": 2",
		"'': 3",
		"'with space': 4",
		"'x[0]': 5",
		"",
	}, "\n")
	d := mustLoad(t, content)
	root := d.Root()
	cases := []struct {
		node *Node
		path string
		text string
	}{
		{root.Field("jobs").Field("build").Field("steps").Index(0).Field("uses"), "jobs.build.steps[0].uses", "actions/checkout@v4"},
		{root.Field("jobs").Field("build").Field("steps").Index(1), "jobs.build.steps[1]", "run: make"},
		{root.Field("a.b").Field("c"), "['a.b'].c", "1"},
		{root.Field("it's"), "['it''s']", "2"},
		{root.Field(""), "['']", "3"},
		{root.Field("with space"), "['with space']", "4"},
		{root.Field("x[0]"), "['x[0]']", "5"},
	}
	for _, tc := range cases {
		if tc.node == nil {
			t.Fatalf("node for %q is nil", tc.path)
		}
		if tc.node.Path() != tc.path {
			t.Errorf("Path = %q, want %q", tc.node.Path(), tc.path)
		}
		back := root.Lookup(tc.path)
		if back == nil || back.Pos() != tc.node.Pos() {
			t.Errorf("Lookup(%q) round trip failed", tc.path)
			continue
		}
		if got := textAt(content, back.Pos()); got != tc.text {
			t.Errorf("Lookup(%q) text = %q, want %q", tc.path, got, tc.text)
		}
	}
	for _, bad := range []string{".jobs", "jobs.", "jobs..build", "jobs[x]", "jobs['a", "jobs[0", "jobsbuild[0]", "jobs.build.steps[9]"} {
		if n := root.Lookup(bad); n != nil {
			t.Errorf("Lookup(%q) = %v, want nil", bad, n.Path())
		}
	}
	if root.Lookup("") != root && root.Lookup("").Pos() != root.Pos() {
		t.Errorf("Lookup(\"\") must return the node itself")
	}
}

func TestOnKeyStaysString(t *testing.T) {
	d := mustLoad(t, "on:\n  push:\nyes: 1\noff: 2\n")
	root := d.Root()
	if got := root.Keys(); !reflect.DeepEqual(got, []string{"on", "yes", "off"}) {
		t.Fatalf("Keys = %v", got)
	}
	if !root.Has("on") || root.Field("on").Kind() != KindMapping {
		t.Fatalf("on key missing or not mapping")
	}
	if root.FieldKey("on").Tag() != "!!str" {
		t.Fatalf("on key tag = %q", root.FieldKey("on").Tag())
	}
}

func TestNilSafety(t *testing.T) {
	d := mustLoad(t, "a: 1\n")
	if d.Root().Field("a").Field("b").Index(3).Field("c").Exists() {
		t.Fatal("missing chain reported as existing")
	}
	var n *Node
	if n.Kind() != KindInvalid || n.Exists() || n.Pos().Valid() || n.Path() != "" || n.Tag() != "" || n.IsNull() || n.Len() != 0 {
		t.Fatal("nil node returned non-zero value")
	}
	if n.Field("x") != nil || n.FieldKey("x") != nil || n.Has("x") || n.Fields() != nil || n.Keys() != nil || n.Items() != nil || n.Index(0) != nil {
		t.Fatal("nil node returned non-nil child")
	}
	if s, ok := n.Str(); ok || s != "" {
		t.Fatal("nil Str")
	}
	if n.StrOr("fb") != "fb" {
		t.Fatal("nil StrOr")
	}
	if _, ok := n.PosStr(); ok {
		t.Fatal("nil PosStr")
	}
	if _, ok := n.Int(); ok {
		t.Fatal("nil Int")
	}
	if _, ok := n.Bool(); ok {
		t.Fatal("nil Bool")
	}
	if n.StrList() != nil || n.Lookup("a") != nil {
		t.Fatal("nil StrList or Lookup")
	}
	n.Walk(func(*Node) bool { t.Fatal("walk visited nil"); return true })
	var doc *Document
	if doc.Root() != nil || !doc.Empty() || doc.LineCount() != 0 {
		t.Fatal("nil document")
	}
}

func TestStrList(t *testing.T) {
	content := "a: main\nb: [x, 'y']\nc:\n  - p\n  - q\nd:\n  k1: 1\n  k2:\ne:\n"
	d := mustLoad(t, content)
	root := d.Root()
	cases := []struct {
		path  string
		want  []string
		texts []string
	}{
		{"a", []string{"main"}, []string{"main"}},
		{"b", []string{"x", "y"}, []string{"x", "'y'"}},
		{"c", []string{"p", "q"}, []string{"p", "q"}},
		{"d", []string{"k1", "k2"}, []string{"k1", "k2"}},
		{"e", nil, nil},
	}
	for _, tc := range cases {
		list := root.Field(tc.path).StrList()
		if len(list) != len(tc.want) {
			t.Fatalf("%s: len = %d, want %d", tc.path, len(list), len(tc.want))
		}
		for i, p := range list {
			if p.Value != tc.want[i] {
				t.Errorf("%s[%d] = %q, want %q", tc.path, i, p.Value, tc.want[i])
			}
			if got := textAt(content, p.Pos); got != tc.texts[i] {
				t.Errorf("%s[%d] text = %q, want %q", tc.path, i, got, tc.texts[i])
			}
		}
	}
}

func TestNullValues(t *testing.T) {
	content := "on:\n  push:\n  pull_request:\n    types: [opened]\n  workflow_dispatch:\n"
	d := mustLoad(t, content)
	on := d.Root().Field("on")
	for _, name := range []string{"push", "workflow_dispatch"} {
		if !on.Has(name) {
			t.Fatalf("Has(%q) = false", name)
		}
		v := on.Field(name)
		if !v.IsNull() {
			t.Fatalf("%s is not null", name)
		}
		if _, ok := v.Str(); ok {
			t.Fatalf("%s Str ok for null", name)
		}
		checkBounds(t, name, d, content, v.Pos())
	}
	if p := on.Field("push").Pos(); p.Line != 2 || p.Column != 7 || p.EndColumn != 7 {
		t.Fatalf("push null position = %v, want clamped to 2:7", p)
	}
	if on.Has("missing") {
		t.Fatal("Has(missing) = true")
	}
}

func TestCRLF(t *testing.T) {
	content := "a: b\r\nc: |\r\n  x\r\n  y\r\nd: 'q'\r\ne: [1, 2]\r\n"
	d := mustLoad(t, content)
	root := d.Root()
	if s, _ := root.Field("a").Str(); s != "b" {
		t.Fatalf("a = %q", s)
	}
	if s, _ := root.Field("c").Str(); s != "x\ny\n" {
		t.Fatalf("c = %q", s)
	}
	if d.LineCount() != 6 {
		t.Fatalf("LineCount = %d", d.LineCount())
	}
	want := map[string]string{"a": "b", "c": "|\n  x\n  y", "d": "'q'", "e": "[1, 2]"}
	for path, text := range want {
		if got := textAt(content, root.Field(path).Pos()); got != text {
			t.Errorf("%s text = %q, want %q", path, got, text)
		}
	}
}

func TestAliasResolution(t *testing.T) {
	content := "defs: &d\n  runs-on: ubuntu\n  list: [a, b]\nuse: *d\nscalar: &s hi\nref: *s\n"
	d := mustLoad(t, content)
	root := d.Root()
	use := root.Field("use")
	if use.Kind() != KindMapping {
		t.Fatalf("alias kind = %v", use.Kind())
	}
	if got := textAt(content, use.Pos()); got != "*d" {
		t.Fatalf("alias position text = %q", got)
	}
	if s, _ := use.Field("runs-on").Str(); s != "ubuntu" {
		t.Fatalf("alias field = %q", s)
	}
	if use.Field("list").Len() != 2 {
		t.Fatalf("alias list len = %d", use.Field("list").Len())
	}
	if use.Field("runs-on").Path() != "use.runs-on" {
		t.Fatalf("alias child path = %q", use.Field("runs-on").Path())
	}
	if use.Field("runs-on").Pos().Line != 2 {
		t.Fatalf("alias child position = %v", use.Field("runs-on").Pos())
	}
	ref := root.Field("ref")
	if s, ok := ref.Str(); !ok || s != "hi" || ref.Tag() != "!!str" {
		t.Fatalf("scalar alias = %q %v %q", s, ok, ref.Tag())
	}
}

func TestMergeKeys(t *testing.T) {
	content := strings.Join([]string{
		"base: &b",
		"  runs-on: ubuntu",
		"  timeout: 5",
		"other: &o",
		"  timeout: 9",
		"  env: x",
		"single:",
		"  <<: *b",
		"  name: single",
		"job:",
		"  <<: [*b, *o]",
		"  runs-on: macos",
		"chain: &c",
		"  <<: *o",
		"  extra: 1",
		"deep:",
		"  <<: *c",
		"",
	}, "\n")
	d := mustLoad(t, content)
	root := d.Root()

	single := root.Field("single")
	if got := single.Keys(); !reflect.DeepEqual(got, []string{"runs-on", "timeout", "name"}) {
		t.Fatalf("single keys = %v", got)
	}
	if s, _ := single.Field("runs-on").Str(); s != "ubuntu" {
		t.Fatalf("single runs-on = %q", s)
	}
	if p := single.Field("runs-on").Pos(); p.Line != 2 || textAt(content, p) != "ubuntu" {
		t.Fatalf("merged field position = %v", p)
	}
	if single.Field("runs-on").Path() != "single.runs-on" {
		t.Fatalf("merged path = %q", single.Field("runs-on").Path())
	}
	if kp := single.FieldKey("timeout").Pos(); kp.Line != 3 {
		t.Fatalf("merged key position = %v", kp)
	}

	job := root.Field("job")
	if got := job.Keys(); !reflect.DeepEqual(got, []string{"timeout", "env", "runs-on"}) {
		t.Fatalf("job keys = %v", got)
	}
	if s, _ := job.Field("runs-on").Str(); s != "macos" {
		t.Fatalf("explicit key must win, got %q", s)
	}
	if v, _ := job.Field("timeout").Int(); v != 5 {
		t.Fatalf("earlier merge source must win, got %d", v)
	}
	if !job.Has("env") || job.Len() != 3 {
		t.Fatalf("job Has(env)=%v Len=%d", job.Has("env"), job.Len())
	}
	for _, k := range job.Keys() {
		if k == "<<" {
			t.Fatal("<< must not appear in Keys")
		}
	}
	if job.Field("<<").Kind() != KindSequence {
		t.Fatalf("Field(<<) kind = %v", job.Field("<<").Kind())
	}

	deep := root.Field("deep")
	if got := deep.Keys(); !reflect.DeepEqual(got, []string{"timeout", "env", "extra"}) {
		t.Fatalf("merge chain keys = %v", got)
	}
	back := root.Lookup("deep.env")
	if back == nil || back.Pos() != deep.Field("env").Pos() {
		t.Fatal("Lookup through merge chain failed")
	}
}

func TestMergeDepthLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("m0: &m0\n  k0: v\n")
	for i := 1; i <= 80; i++ {
		b.WriteString("m")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(": &m")
		b.WriteString(strconv.Itoa(i))
		b.WriteString("\n  <<: *m")
		b.WriteString(strconv.Itoa(i - 1))
		b.WriteString("\n  k")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(": v\n")
	}
	d := mustLoad(t, b.String())
	n := d.Root().Field("m80")
	if n.Has("k0") {
		t.Fatal("merge resolution must stop after depth 64")
	}
	if !n.Has("k20") {
		t.Fatal("merge resolution within depth limit failed")
	}
}

func TestWalk(t *testing.T) {
	content := "a: &x [1, 2]\nb: *x\nc:\n  d: {e: f}\n"
	d := mustLoad(t, content)
	var paths []string
	d.Root().Walk(func(n *Node) bool {
		paths = append(paths, n.Path())
		return true
	})
	want := []string{"", "a", "a[0]", "a[1]", "b", "c", "c.d", "c.d.e"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("walk paths = %v, want %v", paths, want)
	}
	paths = nil
	d.Root().Walk(func(n *Node) bool {
		paths = append(paths, n.Path())
		return n.Path() != "c"
	})
	want = []string{"", "a", "a[0]", "a[1]", "b", "c"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("pruned walk paths = %v, want %v", paths, want)
	}
}

func TestWalkDoesNotExpandAliases(t *testing.T) {
	var b strings.Builder
	b.WriteString("l0: &l0 [a, a, a, a, a, a, a, a, a, a]\n")
	for i := 1; i < 10; i++ {
		b.WriteString("l" + strconv.Itoa(i) + ": &l" + strconv.Itoa(i) + " [")
		for j := 0; j < 10; j++ {
			if j > 0 {
				b.WriteString(", ")
			}
			b.WriteString("*l" + strconv.Itoa(i-1))
		}
		b.WriteString("]\n")
	}
	d := mustLoad(t, b.String())
	count := 0
	d.Root().Walk(func(*Node) bool {
		count++
		return true
	})
	if count > 200 {
		t.Fatalf("walk visited %d nodes; aliases were expanded", count)
	}
}

func TestSyntaxErrorLine(t *testing.T) {
	cases := []struct {
		content string
		line    int
	}{
		{"a: b\n  c: d\n", 2},
		{"x: 1\ny: 2\nz: [\n", 3},
		{"a: 1\nb: 2\nc: \"open\n", 3},
		{"\n\n\na: b\n  c: d\n", 5},
	}
	for _, tc := range cases {
		_, err := Load("bad.yml", []byte(tc.content))
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Fatalf("%q: error %v is not *SyntaxError", tc.content, err)
		}
		lc := len(normalizedLines(tc.content))
		want := tc.line
		if want > lc {
			want = lc
		}
		if se.Line != want {
			t.Errorf("%q: line = %d, want %d (%s)", tc.content, se.Line, want, se.Message)
		}
		if se.File != "bad.yml" || se.Unwrap() == nil || !strings.HasPrefix(se.Error(), "bad.yml:") {
			t.Errorf("%q: bad error fields %+v", tc.content, se)
		}
		if p := se.Pos(); !p.Valid() || p.Line != se.Line {
			t.Errorf("%q: Pos = %v", tc.content, p)
		}
	}
	_, err := Load("bad.yml", []byte("a: *missing\n"))
	var se *SyntaxError
	if !errors.As(err, &se) || se.Line != 0 || se.Pos().Valid() || !strings.Contains(se.Error(), "missing") {
		t.Fatalf("unknown anchor error = %v", err)
	}
}

func TestEmptyDocuments(t *testing.T) {
	for _, content := range []string{"", "# only a comment\n", "# a\n\n# b", "   \n\n"} {
		d, err := Load("e.yml", []byte(content))
		if err != nil {
			t.Fatalf("%q: %v", content, err)
		}
		if !d.Empty() || d.Root() != nil {
			t.Fatalf("%q: not empty", content)
		}
	}
}

func TestScalarConversions(t *testing.T) {
	content := "i: 42\nh: 0x1F\no: 0o17\nneg: -3\nq: '7'\nf: 1.5\nbig: 99999999999999999999999\n" +
		"b1: true\nb2: No\nb3: ON\nb4: y\nb5: 'off'\nb6: maybe\n"
	d := mustLoad(t, content)
	root := d.Root()
	ints := map[string]struct {
		v  int
		ok bool
	}{"i": {42, true}, "h": {31, true}, "o": {15, true}, "neg": {-3, true}, "q": {0, false}, "f": {0, false}, "big": {0, false}}
	for k, want := range ints {
		v, ok := root.Field(k).Int()
		if v != want.v || ok != want.ok {
			t.Errorf("Int(%s) = %d,%v want %d,%v", k, v, ok, want.v, want.ok)
		}
	}
	bools := map[string]struct {
		v  bool
		ok bool
	}{"b1": {true, true}, "b2": {false, true}, "b3": {true, true}, "b4": {true, true}, "b5": {false, true}, "b6": {false, false}}
	for k, want := range bools {
		v, ok := root.Field(k).Bool()
		if v != want.v || ok != want.ok {
			t.Errorf("Bool(%s) = %v,%v want %v,%v", k, v, ok, want.v, want.ok)
		}
	}
	if root.Field("i").StrOr("x") != "42" || root.Field("missing").StrOr("x") != "x" {
		t.Error("StrOr")
	}
	if p, ok := root.Field("q").PosStr(); !ok || p.Value != "7" || textAt(content, p.Pos) != "'7'" {
		t.Errorf("PosStr = %+v %v", p, ok)
	}
}

func TestPositionMethods(t *testing.T) {
	p := Position{File: "f.yml", Line: 2, Column: 3, EndLine: 4, EndColumn: 5, Path: "a"}
	if !p.Valid() {
		t.Fatal("valid position reported invalid")
	}
	invalid := []Position{{}, {Line: 1, Column: 0, EndLine: 1, EndColumn: 1}, {Line: 2, Column: 1, EndLine: 1, EndColumn: 1}, {Line: 1, Column: 5, EndLine: 1, EndColumn: 4}}
	for _, q := range invalid {
		if q.Valid() {
			t.Errorf("%v reported valid", q)
		}
	}
	contains := []struct {
		line, col int
		want      bool
	}{{2, 3, true}, {2, 2, false}, {3, 100, true}, {4, 5, true}, {4, 6, false}, {1, 9, false}, {5, 1, false}}
	for _, c := range contains {
		if got := p.Contains(c.line, c.col); got != c.want {
			t.Errorf("Contains(%d,%d) = %v", c.line, c.col, got)
		}
	}
	q := Position{Line: 1, Column: 7, EndLine: 3, EndColumn: 1}
	cov := p.Cover(q)
	if cov.Line != 1 || cov.Column != 7 || cov.EndLine != 4 || cov.EndColumn != 5 || cov.File != "f.yml" {
		t.Errorf("Cover = %v", cov)
	}
	if p.Cover(Position{}) != p || (Position{}).Cover(p) != p {
		t.Error("Cover with invalid position")
	}
	if p.String() != "f.yml:2:3-4:5" {
		t.Errorf("String = %q", p.String())
	}
	if (Position{Line: 1, Column: 1, EndLine: 1, EndColumn: 2}).String() != "1:1-1:2" {
		t.Error("String without file")
	}
	pos := At(3, p)
	if pos.Value != 3 || pos.Pos != p {
		t.Error("At")
	}
}

func TestBOM(t *testing.T) {
	content := "\ufeffa: b\nc: d\n"
	d := mustLoad(t, content)
	if got := textAt(content, d.Root().Field("a").Pos()); got != "b" {
		t.Fatalf("BOM value text = %q", got)
	}
}

func TestMergeFanOutIsBounded(t *testing.T) {
	var b strings.Builder
	b.WriteString("m0: &m0 {k: v}\n")
	for i := 1; i <= 60; i++ {
		prev := "*m" + strconv.Itoa(i-1)
		b.WriteString("m" + strconv.Itoa(i) + ": &m" + strconv.Itoa(i) + " {<<: [" + prev + ", " + prev + "], x" + strconv.Itoa(i) + ": 1}\n")
	}
	d := mustLoad(t, b.String())
	n := d.Root().Field("m60")
	if got := n.Len(); got != 61 {
		t.Fatalf("m60 has %d fields, want 61", got)
	}
	if s, _ := n.Field("k").Str(); s != "v" {
		t.Fatalf("m60.k = %q", s)
	}
}

func TestLineComment(t *testing.T) {
	doc, err := Load("wf.yml", []byte("steps:\n  - uses: a/b@abc # v4.1.1\n  - uses: c/d@e   #v2\n  - uses: x/y@z\n  - uses: q/r@s  ##  pinned  \n"))
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	steps := doc.Root().Field("steps")
	want := []string{"v4.1.1", "v2", "", "pinned"}
	for i, w := range want {
		if got := steps.Index(i).Field("uses").LineComment(); got != w {
			t.Errorf("step %d LineComment = %q, want %q", i, got, w)
		}
	}
	var nilNode *Node
	if nilNode.LineComment() != "" || doc.Root().Field("missing").LineComment() != "" {
		t.Error("nil LineComment not empty")
	}
}
