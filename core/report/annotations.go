package report

import (
	"slices"

	"github.com/BETAER-08/quanto/core/semdiff"
)

func annotationTitle(kind string) string {
	if slices.Contains(semdiff.Kinds(), kind) {
		return kind
	}
	return inline(kind)
}

func Annotations(diffs []*semdiff.FileDiff) []Annotation {
	out := []Annotation{}
	for _, d := range sortedDiffs(diffs) {
		for _, f := range d.Findings {
			if !f.Pos.Valid() {
				continue
			}
			a := Annotation{
				Path:      d.Path,
				StartLine: f.Pos.Line,
				EndLine:   f.Pos.EndLine,
				Level:     "notice",
				Title:     annotationTitle(f.Kind),
				Message:   Message(f),
			}
			if f.Pos.Line == f.Pos.EndLine {
				a.StartColumn = f.Pos.Column
				a.EndColumn = f.Pos.EndColumn
			}
			out = append(out, a)
		}
	}
	return out
}
