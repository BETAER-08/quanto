package report

import (
	"strings"
	"unicode/utf8"

	"github.com/BETAER-08/quanto/core/semdiff"
)

const footerText = "Static analysis of workflow files only. No code from this pull request was executed."

type fileBlock struct {
	diff     *semdiff.FileDiff
	findings []semdiff.Finding
	table    bool
}

func visibleBlocks(diffs []*semdiff.FileDiff, includeLow bool) []fileBlock {
	var out []fileBlock
	for _, d := range sortedDiffs(diffs) {
		var fs []semdiff.Finding
		for _, f := range d.Findings {
			if includeLow || f.Significance >= semdiff.Normal {
				fs = append(fs, f)
			}
		}
		table := metricsChanged(d)
		if len(fs) == 0 && !table {
			continue
		}
		out = append(out, fileBlock{diff: d, findings: fs, table: table})
	}
	return out
}

func writeBlock(b *strings.Builder, blk fileBlock) {
	b.WriteString("### " + code(blk.diff.Path) + "\n\n")
	if blk.table {
		b.WriteString("| Metric | Before | After |\n")
		b.WriteString("|---|---:|---:|\n")
		for _, r := range metricRows(blk.diff) {
			b.WriteString("| " + r.label + " | " + r.before + " | " + r.after + " |\n")
		}
		b.WriteString("\n")
	}
	if len(blk.findings) > 0 {
		for _, f := range blk.findings {
			b.WriteString("- " + Message(f) + "\n")
		}
		b.WriteString("\n")
	}
}

func footer(meta Meta) string {
	text := footerText
	if meta.HeadSHA != "" {
		text += " Commit " + code(shortSHA(meta.HeadSHA)) + "."
	}
	return "---\n<sub>" + text + "</sub>\n"
}

func notes(b *strings.Builder, omitted int, meta Meta) {
	if omitted > 0 {
		b.WriteString(plural(omitted, "file", "files") + " omitted due to size.\n\n")
	}
	if meta.SkippedFiles > 0 {
		b.WriteString(plural(meta.SkippedFiles, "additional workflow file was", "additional workflow files were") + " not analyzed.\n\n")
	}
}

func renderBody(prefix string, diffs []*semdiff.FileDiff, meta Meta, includeLow bool) string {
	blocks := visibleBlocks(diffs, includeLow)
	head := prefix + "## quanto\n\n"
	if len(blocks) == 0 {
		head += "No execution changes in " + plural(len(sortedDiffs(diffs)), "workflow file", "workflow files") + ".\n\n"
	} else {
		head += "Execution changes in " + plural(len(blocks), "workflow file", "workflow files") + ".\n\n"
	}
	render := func(shown int) string {
		var b strings.Builder
		b.WriteString(head)
		for _, blk := range blocks[:shown] {
			writeBlock(&b, blk)
		}
		notes(&b, len(blocks)-shown, meta)
		b.WriteString(footer(meta))
		return b.String()
	}
	for shown := len(blocks); shown > 0; shown-- {
		if out := render(shown); utf8.RuneCountInString(out) <= MaxBodyRunes {
			return out
		}
	}
	return render(0)
}

func Markdown(diffs []*semdiff.FileDiff, meta Meta) string {
	return renderBody(CommentMarker+"\n", diffs, meta, false)
}

func CheckSummary(diffs []*semdiff.FileDiff, meta Meta) (string, string) {
	total := 0
	for _, d := range sortedDiffs(diffs) {
		total += len(d.Findings)
	}
	if total == 0 {
		var b strings.Builder
		sorted := sortedDiffs(diffs)
		b.WriteString("## quanto\n\n")
		b.WriteString("No execution changes in " + plural(len(sorted), "workflow file", "workflow files") + ".\n\n")
		if len(sorted) > 0 {
			for _, d := range sorted {
				b.WriteString("- " + code(d.Path) + "\n")
			}
			b.WriteString("\n")
		}
		notes(&b, 0, meta)
		b.WriteString(footer(meta))
		return "No execution changes", b.String()
	}
	return plural(total, "execution change", "execution changes"), renderBody("", diffs, meta, true)
}
