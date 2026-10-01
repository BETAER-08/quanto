package report

import (
	"bytes"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"sort"
	"testing"
	"time"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/core/source"
)

var update = flag.Bool("update", false, "update golden files")

const (
	goldenPath = ".github/workflows/ci.yml"
	goldenSHA  = "abc1234def5678901234567890abcdef12345678"
)

type sample struct {
	avg time.Duration
	n   int
}

type fakeDurations map[string]sample

func (f fakeDurations) JobAverage(workflowPath, jobKey string) (time.Duration, int, bool) {
	if workflowPath != goldenPath {
		return 0, 0, false
	}
	s, ok := f[jobKey]
	if !ok {
		return 0, 0, false
	}
	return s.avg, s.n, true
}

var goldenDurations = map[string]semdiff.DurationSource{
	"estimate-with-history": fakeDurations{
		"build": {avg: 5 * time.Minute, n: 10},
		"Test":  {avg: 10 * time.Minute, n: 12},
	},
	"estimate-insufficient": fakeDurations{
		"build": {avg: 5 * time.Minute, n: 10},
		"Test":  {avg: 10 * time.Minute, n: 4},
	},
}

var goldenRoot = filepath.Join("..", "..", "testdata", "golden", "semdiff")

func parseSide(t *testing.T, path string) (*model.Workflow, error, bool) {
	t.Helper()
	content, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil, false
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	doc, err := source.Load(goldenPath, content)
	if err != nil {
		return nil, err, true
	}
	w, _, err := model.Parse(doc)
	if err != nil {
		return nil, err, true
	}
	return w, nil, true
}

func goldenDiff(t *testing.T, name string) *semdiff.FileDiff {
	t.Helper()
	dir := filepath.Join(goldenRoot, name)
	in := semdiff.Input{Path: goldenPath}
	if w, err, ok := parseSide(t, filepath.Join(dir, "before.yml")); ok {
		in.Before, in.BeforeErr = w, err
	}
	if w, err, ok := parseSide(t, filepath.Join(dir, "after.yml")); ok {
		in.After, in.AfterErr = w, err
	}
	return semdiff.Compare(in, semdiff.Options{Durations: goldenDurations[name]})
}

func goldenCaseNames(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(goldenRoot)
	if err != nil {
		t.Fatalf("read golden dir: %v", err)
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out
}

func checkGolden(t *testing.T, path string, got []byte) {
	t.Helper()
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("mismatch with %s\ngot:\n%s\nwant:\n%s", path, got, want)
	}
}

func TestGoldenCount(t *testing.T) {
	if n := len(goldenCaseNames(t)); n != 32 {
		t.Fatalf("golden cases = %d, want 32", n)
	}
}

func TestGolden(t *testing.T) {
	meta := Meta{HeadSHA: goldenSHA}
	for _, name := range goldenCaseNames(t) {
		t.Run(name, func(t *testing.T) {
			diffs := []*semdiff.FileDiff{goldenDiff(t, name)}
			md := Markdown(diffs, meta)
			txt := Text(diffs)
			again := []*semdiff.FileDiff{goldenDiff(t, name)}
			if Markdown(again, meta) != md || Text(again) != txt {
				t.Fatalf("non-deterministic output")
			}
			dir := filepath.Join(goldenRoot, name)
			checkGolden(t, filepath.Join(dir, "expected.md"), []byte(md))
			checkGolden(t, filepath.Join(dir, "expected.txt"), []byte(txt))
		})
	}
}
