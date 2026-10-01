package semdiff

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/BETAER-08/quanto/core/model"
)

func corpusWorkflows(t *testing.T) ([]string, []*model.Workflow) {
	t.Helper()
	dir := filepath.Join("..", "..", "testdata", "corpus")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		t.Fatalf("read corpus: %v", err)
	}
	var names []string
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !(strings.HasSuffix(name, ".yml") || strings.HasSuffix(name, ".yaml")) {
			continue
		}
		names = append(names, name)
	}
	sort.Strings(names)
	var outNames []string
	var out []*model.Workflow
	for _, name := range names {
		content, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		w, err := parseWorkflow(name, content)
		if err != nil {
			t.Errorf("%s: %v", name, err)
			continue
		}
		outNames = append(outNames, name)
		out = append(out, w)
	}
	return outNames, out
}

var symmetricPairs = []struct {
	left  []string
	right []string
}{
	{[]string{kindTriggerAdded, kindTriggerPRTargetAdded}, []string{kindTriggerRemoved}},
	{[]string{kindJobAdded}, []string{kindJobRemoved}},
	{[]string{kindActionAdded, kindActionThirdPartyAdded}, []string{kindActionRemoved}},
	{[]string{kindWorkflowAdded}, []string{kindWorkflowRemoved}},
	{[]string{kindJobRenamed}, []string{kindJobRenamed}},
}

func sumKinds(counts map[string]int, kinds []string) int {
	n := 0
	for _, k := range kinds {
		n += counts[k]
	}
	return n
}

func TestCorpusIdentity(t *testing.T) {
	names, wfs := corpusWorkflows(t)
	if len(wfs) == 0 {
		t.Skip("testdata/corpus is empty; run scripts/fetch-corpus.sh")
	}
	for i, w := range wfs {
		d := Compare(Input{Path: names[i], Before: w, After: w}, Options{})
		if len(d.Findings) != 0 {
			t.Errorf("%s: Compare(a, a) findings = %v", names[i], kindList(d))
		}
		t.Logf("%s: jobs=%s depth=%d width=%s", names[i], d.After.JobsPerRun, d.After.Depth, d.After.Width)
	}
}

func TestCorpusSymmetry(t *testing.T) {
	names, wfs := corpusWorkflows(t)
	if len(wfs) < 2 {
		t.Skip("testdata/corpus has fewer than two files; run scripts/fetch-corpus.sh")
	}
	for i := 0; i+1 < len(wfs); i++ {
		a, b := wfs[i], wfs[i+1]
		dab := Compare(Input{Path: "pair.yml", Before: a, After: b}, Options{})
		dba := Compare(Input{Path: "pair.yml", Before: b, After: a}, Options{})
		checkPermissionCoverage(t, a, b, dab)
		checkPermissionCoverage(t, b, a, dba)
		ab, ba := countKinds(dab), countKinds(dba)
		for _, p := range symmetricPairs {
			if l, r := sumKinds(ab, p.left), sumKinds(ba, p.right); l != r {
				t.Errorf("%s -> %s: %v = %d, reverse %v = %d", names[i], names[i+1], p.left, l, p.right, r)
			}
		}
	}
}
