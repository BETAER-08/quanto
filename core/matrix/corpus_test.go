package matrix

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

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

func TestCorpusMatrix(t *testing.T) {
	files := corpusFiles(t)
	if len(files) == 0 {
		t.Skip("testdata/corpus is empty; run scripts/fetch-corpus.sh")
	}
	totalMatrices := 0
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
		instances, matrices, dynamic, large := 0, 0, 0, 0
		for _, job := range w.Jobs {
			var node *source.Node
			if job.Strategy != nil {
				node = job.Strategy.Matrix
			}
			if node != nil {
				matrices++
			}
			exp, err := Expand(node)
			if err != nil {
				t.Errorf("%s: job %s: %v", name, job.ID, err)
				continue
			}
			switch {
			case exp.Dynamic:
				dynamic++
			case !exp.Materialized:
				large++
			default:
				instances += exp.Count
			}
			for _, d := range exp.Diagnostics {
				t.Logf("%s: job %s: %s %s at %s", name, job.ID, d.Code, d.Message, d.Pos)
			}
		}
		totalMatrices += matrices
		t.Logf("%s: jobs=%d matrices=%d instances=%d dynamic=%d unmaterialized=%d", name, len(w.Jobs), matrices, instances, dynamic, large)
	}
	t.Logf("corpus files=%d matrices=%d", len(files), totalMatrices)
}
