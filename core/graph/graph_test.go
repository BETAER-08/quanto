package graph

import (
	"math"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

func workflow(t *testing.T, content string) *model.Workflow {
	t.Helper()
	doc, err := source.Load("graph.yml", []byte(content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	w, _, err := model.Parse(doc)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return w
}

func build(t *testing.T, jobs string) *Graph {
	t.Helper()
	return Build(workflow(t, "on: push\njobs:\n"+jobs))
}

const linear = `  c:
    needs: b
    runs-on: x
  b:
    needs: [a]
    runs-on: x
  a:
    runs-on: x
`

const diamond = `  top:
    runs-on: x
  left:
    needs: top
    runs-on: x
  right:
    needs: top
    runs-on: x
  bottom:
    needs: [left, right]
    runs-on: x
`

func TestLinear(t *testing.T) {
	g := build(t, linear)
	if !reflect.DeepEqual(g.Jobs, []string{"a", "b", "c"}) {
		t.Errorf("Jobs = %v", g.Jobs)
	}
	if !reflect.DeepEqual(g.Levels, map[string]int{"a": 0, "b": 1, "c": 2}) {
		t.Errorf("Levels = %v", g.Levels)
	}
	if !reflect.DeepEqual(g.Needs["c"], []string{"b"}) || !reflect.DeepEqual(g.Dependents["a"], []string{"b"}) {
		t.Errorf("Needs = %v, Dependents = %v", g.Needs, g.Dependents)
	}
	if g.Depth() != 3 || g.Width(nil) != 1 {
		t.Errorf("Depth = %d, Width = %d", g.Depth(), g.Width(nil))
	}
	if len(g.Cycles) != 0 || len(g.Unresolved) != 0 {
		t.Errorf("Cycles = %v, Unresolved = %v", g.Cycles, g.Unresolved)
	}
}

func TestDiamond(t *testing.T) {
	g := build(t, diamond)
	want := map[string]int{"top": 0, "left": 1, "right": 1, "bottom": 2}
	if !reflect.DeepEqual(g.Levels, want) {
		t.Errorf("Levels = %v", g.Levels)
	}
	if !reflect.DeepEqual(g.Dependents["top"], []string{"left", "right"}) {
		t.Errorf("Dependents[top] = %v", g.Dependents["top"])
	}
	if !reflect.DeepEqual(g.Needs["bottom"], []string{"left", "right"}) {
		t.Errorf("Needs[bottom] = %v", g.Needs["bottom"])
	}
	if g.Depth() != 3 || g.Width(nil) != 2 {
		t.Errorf("Depth = %d, Width = %d", g.Depth(), g.Width(nil))
	}
}

func TestParallel(t *testing.T) {
	g := build(t, "  a:\n    runs-on: x\n  b:\n    runs-on: x\n  c:\n    runs-on: x\n")
	if g.Depth() != 1 || g.Width(nil) != 3 {
		t.Errorf("Depth = %d, Width = %d", g.Depth(), g.Width(nil))
	}
}

func TestEmpty(t *testing.T) {
	g := Build(&model.Workflow{})
	if g.Depth() != 0 || g.Width(nil) != 0 || len(g.Jobs) != 0 {
		t.Errorf("Depth = %d, Width = %d, Jobs = %v", g.Depth(), g.Width(nil), g.Jobs)
	}
	path, total, ok := g.CriticalPath(nil)
	if path != nil || total != 0 || !ok {
		t.Errorf("CriticalPath = %v %v %v", path, total, ok)
	}
	if g := Build(nil); g.Depth() != 0 {
		t.Errorf("Build(nil).Depth() = %d", g.Depth())
	}
}

func TestUnresolved(t *testing.T) {
	g := build(t, "  b:\n    needs: [zeta, a, alpha]\n    runs-on: x\n  a:\n    needs: ghost\n    runs-on: x\n")
	want := []Edge{{"a", "ghost"}, {"b", "alpha"}, {"b", "zeta"}}
	if !reflect.DeepEqual(g.Unresolved, want) {
		t.Errorf("Unresolved = %v", g.Unresolved)
	}
	if !reflect.DeepEqual(g.Needs["b"], []string{"a"}) {
		t.Errorf("Needs[b] = %v", g.Needs["b"])
	}
	if !reflect.DeepEqual(g.Levels, map[string]int{"a": 0, "b": 1}) {
		t.Errorf("Levels = %v", g.Levels)
	}
}

func TestSelfCycle(t *testing.T) {
	g := build(t, "  a:\n    needs: a\n    runs-on: x\n  b:\n    runs-on: x\n")
	if !reflect.DeepEqual(g.Cycles, [][]string{{"a"}}) {
		t.Errorf("Cycles = %v", g.Cycles)
	}
	if _, ok := g.Levels["a"]; ok {
		t.Errorf("Levels = %v", g.Levels)
	}
	if !reflect.DeepEqual(g.Levels, map[string]int{"b": 0}) {
		t.Errorf("Levels = %v", g.Levels)
	}
}

func TestMultipleCycles(t *testing.T) {
	g := build(t, `  z:
    needs: y
    runs-on: x
  y:
    needs: x
    runs-on: x
  x:
    needs: z
    runs-on: x
  q:
    needs: p
    runs-on: x
  p:
    needs: q
    runs-on: x
  after:
    needs: p
    runs-on: x
  root:
    runs-on: x
`)
	want := [][]string{{"p", "q"}, {"x", "z", "y"}}
	if !reflect.DeepEqual(g.Cycles, want) {
		t.Errorf("Cycles = %v, want %v", g.Cycles, want)
	}
	if !reflect.DeepEqual(g.Levels, map[string]int{"root": 0}) {
		t.Errorf("Levels = %v", g.Levels)
	}
	if g.Depth() != 1 {
		t.Errorf("Depth = %d", g.Depth())
	}
}

func TestWeightedWidth(t *testing.T) {
	g := build(t, diamond)
	if w := g.Width(map[string]int{"left": 6, "right": 4, "bottom": 20}); w != 20 {
		t.Errorf("Width = %d", w)
	}
	if w := g.Width(map[string]int{"left": 6, "right": 4}); w != 10 {
		t.Errorf("Width = %d", w)
	}
	if w := g.Width(map[string]int{"left": 0, "right": 0}); w != 1 {
		t.Errorf("Width = %d", w)
	}
	if w := g.Width(map[string]int{"left": math.MaxInt, "right": math.MaxInt}); w != math.MaxInt {
		t.Errorf("saturated Width = %d", w)
	}
	if w := g.Width(map[string]int{"left": math.MaxInt, "right": 1}); w != math.MaxInt {
		t.Errorf("saturated Width = %d", w)
	}
	if w := g.Width(map[string]int{"left": math.MaxInt - 5, "right": 3}); w != math.MaxInt-2 {
		t.Errorf("near-limit Width = %d", w)
	}
}

func TestCriticalPath(t *testing.T) {
	g := build(t, diamond)
	durations := map[string]time.Duration{
		"top":    time.Minute,
		"left":   5 * time.Minute,
		"right":  3 * time.Minute,
		"bottom": 2 * time.Minute,
	}
	path, total, ok := g.CriticalPath(durations)
	if !reflect.DeepEqual(path, []string{"top", "left", "bottom"}) || total != 8*time.Minute || !ok {
		t.Errorf("CriticalPath = %v %v %v", path, total, ok)
	}
	durations["right"] = 5 * time.Minute
	path, _, _ = g.CriticalPath(durations)
	if !reflect.DeepEqual(path, []string{"top", "left", "bottom"}) {
		t.Errorf("tie path = %v", path)
	}
	durations["right"] = 7 * time.Minute
	path, total, _ = g.CriticalPath(durations)
	if !reflect.DeepEqual(path, []string{"top", "right", "bottom"}) || total != 10*time.Minute {
		t.Errorf("CriticalPath = %v %v", path, total)
	}
	delete(durations, "bottom")
	path, total, ok = g.CriticalPath(durations)
	if ok || !reflect.DeepEqual(path, []string{"top", "right", "bottom"}) || total != 8*time.Minute {
		t.Errorf("CriticalPath with missing duration = %v %v %v", path, total, ok)
	}
	_, _, ok = g.CriticalPath(map[string]time.Duration{"top": time.Minute, "left": time.Hour, "bottom": time.Minute})
	if !ok {
		t.Error("CriticalPath through complete left branch ok = false")
	}
}

func TestCriticalPathIgnoresOffPathMissing(t *testing.T) {
	g := build(t, "  a:\n    runs-on: x\n  b:\n    runs-on: x\n")
	path, total, ok := g.CriticalPath(map[string]time.Duration{"b": time.Minute})
	if !reflect.DeepEqual(path, []string{"b"}) || total != time.Minute || !ok {
		t.Errorf("CriticalPath = %v %v %v", path, total, ok)
	}
	path, _, ok = g.CriticalPath(map[string]time.Duration{})
	if !reflect.DeepEqual(path, []string{"a"}) || ok {
		t.Errorf("CriticalPath = %v %v", path, ok)
	}
}

func TestDeterminism(t *testing.T) {
	content := `  z:
    needs: [y, x]
    runs-on: x
  y:
    needs: x
    runs-on: x
  x:
    needs: z
    runs-on: x
  m:
    needs: [n, ghost]
    runs-on: x
  n:
    needs: m
    runs-on: x
`
	first := build(t, content)
	for i := 0; i < 20; i++ {
		g := build(t, content)
		if !reflect.DeepEqual(g, first) {
			t.Fatalf("run %d differs: %+v vs %+v", i, g, first)
		}
	}
	for _, c := range first.Cycles {
		if c[0] != minOf(c) {
			t.Errorf("cycle %v does not start at its minimum", c)
		}
	}
	if !sort.SliceIsSorted(first.Cycles, func(a, b int) bool { return lessPath(first.Cycles[a], first.Cycles[b]) }) {
		t.Errorf("Cycles not sorted: %v", first.Cycles)
	}
}

func minOf(s []string) string {
	m := s[0]
	for _, v := range s {
		if v < m {
			m = v
		}
	}
	return m
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

func TestCorpusGraph(t *testing.T) {
	files := corpusFiles(t)
	if len(files) == 0 {
		t.Skip("testdata/corpus is empty; run scripts/fetch-corpus.sh")
	}
	for _, file := range files {
		name := filepath.Base(file)
		content, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		doc, err := source.Load(name, content)
		if err != nil {
			t.Errorf("%s: load: %v", name, err)
			continue
		}
		w, _, err := model.Parse(doc)
		if err != nil {
			t.Errorf("%s: parse: %v", name, err)
			continue
		}
		g := Build(w)
		if len(g.Cycles) != 0 {
			t.Errorf("%s: cycles %v", name, g.Cycles)
		}
		if len(g.Unresolved) != 0 {
			t.Errorf("%s: unresolved %v", name, g.Unresolved)
		}
		if len(g.Levels) != len(g.Jobs) {
			t.Errorf("%s: %d of %d jobs have levels", name, len(g.Levels), len(g.Jobs))
		}
		t.Logf("%s: jobs=%d depth=%d width=%d", name, len(g.Jobs), g.Depth(), g.Width(nil))
	}
}
