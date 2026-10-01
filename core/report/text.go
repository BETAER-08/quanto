package report

import (
	"strings"

	"github.com/BETAER-08/quanto/core/semdiff"
)

func Text(diffs []*semdiff.FileDiff) string {
	sorted := sortedDiffs(diffs)
	if len(sorted) == 0 {
		return "No workflow files\n"
	}
	var b strings.Builder
	for i, d := range sorted {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(inline(d.Path) + " (" + string(d.Status))
		if d.OldPath != "" && d.OldPath != d.Path {
			b.WriteString(", from " + inline(d.OldPath))
		}
		b.WriteString(")\n")
		for _, r := range metricRows(d) {
			if r.before != r.after {
				b.WriteString("  " + r.label + ": " + r.before + " → " + r.after + "\n")
			}
		}
		if len(d.Findings) == 0 {
			b.WriteString("  No execution changes\n")
			continue
		}
		for _, f := range d.Findings {
			b.WriteString("  - [" + f.Significance.String() + "] " + Message(f) + "\n")
		}
	}
	return b.String()
}
