package matrix

import (
	"testing"

	"github.com/BETAER-08/quanto/core/source"
)

func load(t *testing.T, content string) *source.Node {
	t.Helper()
	doc, err := source.Load("matrix.yml", []byte(content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return doc.Root()
}

func expand(t *testing.T, content string) *Expansion {
	t.Helper()
	exp, err := Expand(load(t, content))
	if err != nil {
		t.Fatalf("Expand: %v", err)
	}
	return exp
}

func instanceCanons(exp *Expansion) []string {
	out := make([]string, len(exp.Instances))
	for i, inst := range exp.Instances {
		out[i] = canon(inst.Values)
	}
	return out
}

func assertInstances(t *testing.T, exp *Expansion, want []string) {
	t.Helper()
	got := instanceCanons(exp)
	if len(got) != len(want) {
		t.Fatalf("instances = %d %v, want %d %v", len(got), got, len(want), want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("instance %d = %s, want %s", i, got[i], want[i])
		}
	}
	if exp.Count != len(want) {
		t.Errorf("Count = %d, want %d", exp.Count, len(want))
	}
}

func TestGitHubDocsInclude(t *testing.T) {
	exp := expand(t, `fruit: [apple, pear]
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
`)
	assertInstances(t, exp, []string{
		`{"animal":"cat","color":"pink","fruit":"apple","shape":"circle"}`,
		`{"animal":"dog","color":"green","fruit":"apple","shape":"circle"}`,
		`{"animal":"cat","color":"pink","fruit":"pear"}`,
		`{"animal":"dog","color":"green","fruit":"pear"}`,
		`{"fruit":"banana"}`,
		`{"animal":"cat","fruit":"banana"}`,
	})
	wantFrom := []bool{false, false, false, false, true, true}
	for i, inst := range exp.Instances {
		if inst.FromInclude != wantFrom[i] {
			t.Errorf("instance %d FromInclude = %v, want %v", i, inst.FromInclude, wantFrom[i])
		}
	}
	if !exp.Materialized || exp.Dynamic {
		t.Errorf("Materialized = %v, Dynamic = %v", exp.Materialized, exp.Dynamic)
	}
	if len(exp.Axes) != 2 || exp.Axes[0] != "fruit" || exp.Axes[1] != "animal" {
		t.Errorf("Axes = %v", exp.Axes)
	}
	if len(exp.Diagnostics) != 0 {
		t.Errorf("Diagnostics = %v", exp.Diagnostics)
	}
}

func TestExcludeDocs(t *testing.T) {
	exp := expand(t, `os: [macos-latest, windows-latest]
version: [12, 14, 16]
environment: [staging, production]
exclude:
  - os: macos-latest
    version: 12
    environment: production
  - os: windows-latest
    version: 16
`)
	assertInstances(t, exp, []string{
		`{"environment":"staging","os":"macos-latest","version":12}`,
		`{"environment":"staging","os":"macos-latest","version":14}`,
		`{"environment":"production","os":"macos-latest","version":14}`,
		`{"environment":"staging","os":"macos-latest","version":16}`,
		`{"environment":"production","os":"macos-latest","version":16}`,
		`{"environment":"staging","os":"windows-latest","version":12}`,
		`{"environment":"production","os":"windows-latest","version":12}`,
		`{"environment":"staging","os":"windows-latest","version":14}`,
		`{"environment":"production","os":"windows-latest","version":14}`,
	})
}

func TestCartesianOrder(t *testing.T) {
	exp := expand(t, "version: [10, 12]\nos: [a, b]\n")
	assertInstances(t, exp, []string{
		`{"os":"a","version":10}`,
		`{"os":"b","version":10}`,
		`{"os":"a","version":12}`,
		`{"os":"b","version":12}`,
	})
	if exp.Axes[0] != "version" || exp.Axes[1] != "os" {
		t.Errorf("Axes = %v", exp.Axes)
	}
}

func TestIncludeOnly(t *testing.T) {
	exp := expand(t, `include:
  - os: ubuntu
    node: 18
  - os: windows
`)
	assertInstances(t, exp, []string{
		`{"node":18,"os":"ubuntu"}`,
		`{"os":"windows"}`,
	})
	for i, inst := range exp.Instances {
		if !inst.FromInclude {
			t.Errorf("instance %d FromInclude = false", i)
		}
	}
	if len(exp.Axes) != 0 {
		t.Errorf("Axes = %v", exp.Axes)
	}
}

func TestIncludeNotMergedIntoIncludeCreated(t *testing.T) {
	exp := expand(t, `os: [linux]
include:
  - os: mac
  - os: mac
    arch: arm
  - extra: x
`)
	assertInstances(t, exp, []string{
		`{"extra":"x","os":"linux"}`,
		`{"os":"mac"}`,
		`{"arch":"arm","os":"mac"}`,
	})
}

func TestIncludeOverwritesPreviousIncludeKeys(t *testing.T) {
	exp := expand(t, `os: [linux, mac]
include:
  - tag: one
  - os: mac
    tag: two
`)
	assertInstances(t, exp, []string{
		`{"os":"linux","tag":"one"}`,
		`{"os":"mac","tag":"two"}`,
	})
}

func TestIncludeAfterExclude(t *testing.T) {
	exp := expand(t, `os: [linux, mac]
exclude:
  - os: mac
include:
  - os: mac
    tag: x
`)
	assertInstances(t, exp, []string{
		`{"os":"linux"}`,
		`{"os":"mac","tag":"x"}`,
	})
	if exp.Instances[0].FromInclude || !exp.Instances[1].FromInclude {
		t.Errorf("FromInclude flags = %v %v", exp.Instances[0].FromInclude, exp.Instances[1].FromInclude)
	}
}

func TestExcludePartialAndUnknownKey(t *testing.T) {
	exp := expand(t, `os: [linux, mac]
node: [18, 20]
exclude:
  - node: 20
  - other: x
`)
	assertInstances(t, exp, []string{
		`{"node":18,"os":"linux"}`,
		`{"node":18,"os":"mac"}`,
	})
}

func codes(exp *Expansion) []string {
	out := make([]string, len(exp.Diagnostics))
	for i, d := range exp.Diagnostics {
		out[i] = d.Code
	}
	return out
}

func hasCode(exp *Expansion, code string) bool {
	for _, d := range exp.Diagnostics {
		if d.Code == code {
			return true
		}
	}
	return false
}

func TestEmptyAxis(t *testing.T) {
	exp := expand(t, "os: []\nnode: [18]\n")
	assertInstances(t, exp, nil)
	if !hasCode(exp, "MATRIX-EMPTY-AXIS") {
		t.Errorf("codes = %v", codes(exp))
	}
	d := exp.Diagnostics[0]
	if d.Pos.Line != 1 || d.Pos.Column != 1 {
		t.Errorf("diagnostic pos = %v", d.Pos)
	}
	exp = expand(t, "os: []\ninclude:\n  - os: x\n")
	assertInstances(t, exp, []string{`{"os":"x"}`})
	if !hasCode(exp, "MATRIX-EMPTY-AXIS") {
		t.Errorf("codes = %v", codes(exp))
	}
}

func TestEmptyMatrix(t *testing.T) {
	for _, content := range []string{"{}\n", "include: []\n", "exclude:\n  - os: a\n"} {
		exp := expand(t, content)
		if exp.Count != 0 || len(exp.Instances) != 0 {
			t.Errorf("%q: Count = %d", content, exp.Count)
		}
		if !hasCode(exp, "MATRIX-EMPTY") {
			t.Errorf("%q: codes = %v", content, codes(exp))
		}
	}
}

func TestNilMatrix(t *testing.T) {
	exp, err := Expand(nil)
	if err != nil {
		t.Fatalf("Expand(nil): %v", err)
	}
	if exp.Count != 1 || len(exp.Instances) != 1 || !exp.Materialized || len(exp.Instances[0].Values) != 0 {
		t.Errorf("Expand(nil) = %+v", exp)
	}
}

func TestDynamic(t *testing.T) {
	cases := []struct {
		name    string
		content string
		reason  string
	}{
		{"whole", "${{ fromJSON(needs.setup.outputs.matrix) }}\n", "matrix is an expression"},
		{"axis", "os: ${{ fromJSON(vars.OS) }}\nnode: [18]\n", "matrix axis `os` is an expression"},
		{"include", "os: [a]\ninclude: ${{ fromJSON(vars.INC) }}\n", "matrix include is an expression"},
		{"exclude", "os: [a]\nexclude: ${{ fromJSON(vars.EXC) }}\n", "matrix exclude is an expression"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			exp := expand(t, tc.content)
			if !exp.Dynamic || exp.Materialized || exp.Count != 0 || exp.Instances != nil {
				t.Errorf("exp = %+v", exp)
			}
			if exp.DynamicReason != tc.reason {
				t.Errorf("DynamicReason = %q, want %q", exp.DynamicReason, tc.reason)
			}
		})
	}
}

func TestExpressionElementIsNotDynamic(t *testing.T) {
	exp := expand(t, "os: [ubuntu, '${{ vars.X }}']\n")
	if exp.Dynamic {
		t.Fatal("Dynamic = true")
	}
	assertInstances(t, exp, []string{`{"os":"ubuntu"}`, `{"os":"${{ vars.X }}"}`})
}

func TestOverGitHubLimit(t *testing.T) {
	exp := expand(t, "a: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]\nb: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]\ninclude:\n  - c: 1\n    a: 99\n")
	if exp.Count != 257 || !exp.Materialized || len(exp.Instances) != 257 {
		t.Fatalf("Count = %d, Materialized = %v", exp.Count, exp.Materialized)
	}
	if !hasCode(exp, "MATRIX-OVER-LIMIT") || hasCode(exp, "MATRIX-TOO-LARGE") {
		t.Errorf("codes = %v", codes(exp))
	}
	exp = expand(t, "a: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]\nb: [1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16]\n")
	if exp.Count != 256 || hasCode(exp, "MATRIX-OVER-LIMIT") {
		t.Errorf("Count = %d, codes = %v", exp.Count, codes(exp))
	}
}

func TestTooLarge(t *testing.T) {
	axis := "[0, 1, 2, 3, 4, 5, 6, 7, 8, 9]"
	content := ""
	for _, name := range []string{"a", "b", "c", "d", "e"} {
		content += name + ": " + axis + "\n"
	}
	exp := expand(t, content)
	if !exp.Materialized || exp.Count != 100000 {
		t.Fatalf("at limit: Count = %d, Materialized = %v", exp.Count, exp.Materialized)
	}
	for _, name := range []string{"f", "g", "h", "i", "j", "k", "l", "m", "n", "o", "p", "q", "r", "s", "t", "u", "v", "w", "x", "y", "z"} {
		content += name + ": " + axis + "\n"
	}
	exp = expand(t, content)
	if exp.Materialized || exp.Instances != nil {
		t.Fatalf("Materialized = %v", exp.Materialized)
	}
	if exp.Count <= MaterializeLimit {
		t.Errorf("Count = %d", exp.Count)
	}
	if !hasCode(exp, "MATRIX-TOO-LARGE") || !hasCode(exp, "MATRIX-OVER-LIMIT") {
		t.Errorf("codes = %v", codes(exp))
	}
	if exp.Count != 1000000 {
		t.Errorf("Count = %d, want first product above limit 1000000", exp.Count)
	}
}

func TestObjectAxis(t *testing.T) {
	exp := expand(t, `cfg:
  - {os: linux, flags: [a, b]}
  - os: mac
    flags: []
mode: [x]
include:
  - cfg: {flags: [a, b], os: linux}
    tag: hit
exclude:
  - cfg:
      os: mac
      flags: []
`)
	assertInstances(t, exp, []string{
		`{"cfg":{"flags":["a","b"],"os":"linux"},"mode":"x","tag":"hit"}`,
	})
}

func TestTypeStrictEquality(t *testing.T) {
	exp := expand(t, `node: [18, '18', 18.5, true, null]
exclude:
  - node: '18'
include:
  - node: 18
    tag: int
  - node: 'true'
    tag: str
`)
	assertInstances(t, exp, []string{
		`{"node":18,"tag":"int"}`,
		`{"node":18.5}`,
		`{"node":true}`,
		`{"node":null}`,
		`{"node":"true","tag":"str"}`,
	})
	if _, ok := exp.Instances[0].Values["node"].(int64); !ok {
		t.Errorf("node type = %T", exp.Instances[0].Values["node"])
	}
	if _, ok := exp.Instances[1].Values["node"].(float64); !ok {
		t.Errorf("node type = %T", exp.Instances[1].Values["node"])
	}
}

func TestAnchorsAndAliases(t *testing.T) {
	exp := expand(t, `os: &oses [linux, mac]
again: *oses
`)
	if exp.Count != 4 {
		t.Errorf("Count = %d", exp.Count)
	}
}

func TestErrors(t *testing.T) {
	for _, content := range []string{
		"[a, b]\n",
		"plain\n",
		"os: ubuntu\n",
		"os: {a: b}\n",
		"os:\n",
		"os: [a]\ninclude: {a: b}\n",
		"os: [a]\ninclude: [a]\n",
		"os: [a]\nexclude: [1]\n",
	} {
		if _, err := Expand(load(t, content)); err == nil {
			t.Errorf("%q: Expand returned nil error", content)
		}
	}
}

func TestDeterminism(t *testing.T) {
	content := `fruit: [apple, pear]
animal: [cat, dog]
include:
  - color: green
  - fruit: banana
`
	first := instanceCanons(expand(t, content))
	for i := 0; i < 20; i++ {
		got := instanceCanons(expand(t, content))
		for j := range first {
			if got[j] != first[j] {
				t.Fatalf("run %d instance %d = %s, want %s", i, j, got[j], first[j])
			}
		}
	}
}

func TestCanon(t *testing.T) {
	cases := []struct {
		v    any
		want string
	}{
		{nil, "null"},
		{int64(18), "18"},
		{"18", `"18"`},
		{1.5, "1.5"},
		{true, "true"},
		{[]any{"a", int64(1)}, `["a",1]`},
		{map[string]any{"b": int64(1), "a": "x"}, `{"a":"x","b":1}`},
	}
	for _, tc := range cases {
		if got := canon(tc.v); got != tc.want {
			t.Errorf("canon(%v) = %s, want %s", tc.v, got, tc.want)
		}
	}
}

func FuzzExpand(f *testing.F) {
	seeds := []string{
		"fruit: [apple, pear]\nanimal: [cat, dog]\ninclude:\n  - color: green\n  - fruit: banana\n",
		"os: [a, b]\nexclude:\n  - os: a\n",
		"${{ matrix }}\n",
		"include:\n  - a: 1\n",
		"a: []\n",
		"a: &x [1, 2]\nb: *x\n",
		"[",
		"",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, content string) {
		doc, err := source.Load("fuzz.yml", []byte(content))
		if err != nil || doc.Empty() {
			return
		}
		exp, err := Expand(doc.Root())
		if err != nil {
			return
		}
		if exp.Materialized && exp.Count != len(exp.Instances) {
			t.Fatalf("Count = %d, instances = %d", exp.Count, len(exp.Instances))
		}
		if exp.Dynamic && (exp.Materialized || exp.Count != 0) {
			t.Fatalf("dynamic expansion has Count = %d", exp.Count)
		}
		for _, d := range exp.Diagnostics {
			if !d.Pos.Valid() {
				t.Fatalf("diagnostic %s has invalid position", d.Code)
			}
		}
	})
}
