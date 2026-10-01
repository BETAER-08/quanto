package semdiff

import (
	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

func parseWorkflow(file string, content []byte) (*model.Workflow, error) {
	doc, err := source.Load(file, content)
	if err != nil {
		return nil, err
	}
	w, _, err := model.Parse(doc)
	if err != nil {
		return nil, err
	}
	return w, nil
}

func countKinds(d *FileDiff) map[string]int {
	out := make(map[string]int)
	for _, f := range d.Findings {
		out[f.Kind]++
	}
	return out
}

func kindList(d *FileDiff) []string {
	out := make([]string, 0, len(d.Findings))
	for _, f := range d.Findings {
		out = append(out, f.Kind)
	}
	return out
}
