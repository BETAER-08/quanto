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
	b.WriteString("### " + inline(blk.diff.Path) + "\n\n")
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
		text += " Commit " + inline(shortSHA(meta.HeadSHA)) + "."
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

type sharedLine struct {
	message string
	files   int
}

func shareAcrossFiles(blocks []fileBlock) ([]fileBlock, []sharedLine, int) {
	filesOf := make(map[string]map[int]bool)
	var order []string
	for i, blk := range blocks {
		for _, f := range blk.findings {
			m := Message(f)
			if filesOf[m] == nil {
				filesOf[m] = make(map[int]bool)
				order = append(order, m)
			}
			filesOf[m][i] = true
		}
	}
	var shared []sharedLine
	involved := make(map[int]bool)
	for _, m := range order {
		if len(filesOf[m]) < 2 {
			continue
		}
		shared = append(shared, sharedLine{message: m, files: len(filesOf[m])})
		for i := range filesOf[m] {
			involved[i] = true
		}
	}
	if len(shared) == 0 {
		return blocks, nil, 0
	}
	var out []fileBlock
	for _, blk := range blocks {
		var kept []semdiff.Finding
		for _, f := range blk.findings {
			if len(filesOf[Message(f)]) < 2 {
				kept = append(kept, f)
			}
		}
		if len(kept) == 0 && !blk.table {
			continue
		}
		out = append(out, fileBlock{diff: blk.diff, findings: kept, table: blk.table})
	}
	return out, shared, len(involved)
}

func writeShared(b *strings.Builder, shared []sharedLine, files int) {
	if len(shared) == 0 {
		return
	}
	b.WriteString("### Across " + plural(files, "workflow file", "workflow files") + "\n\n")
	for _, l := range shared {
		b.WriteString("- " + l.message + " (" + plural(l.files, "file", "files") + ")\n")
	}
	b.WriteString("\n")
}

func renderBody(prefix string, diffs []*semdiff.FileDiff, meta Meta, includeLow bool) string {
	visible := visibleBlocks(diffs, includeLow)
	blocks, shared, sharedFiles := shareAcrossFiles(visible)
	head := prefix + "## quanto\n\n"
	if len(visible) == 0 {
		head += "No execution changes in " + plural(len(sortedDiffs(diffs)), "workflow file", "workflow files") + ".\n\n"
	} else {
		head += "Execution changes in " + plural(len(visible), "workflow file", "workflow files") + ".\n\n"
	}
	render := func(shown int) string {
		var b strings.Builder
		b.WriteString(head)
		writeShared(&b, shared, sharedFiles)
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
				b.WriteString("- " + inline(d.Path) + "\n")
			}
			b.WriteString("\n")
		}
		notes(&b, 0, meta)
		b.WriteString(footer(meta))
		return "No execution changes", b.String()
	}
	return plural(total, "execution change", "execution changes"), renderBody("", diffs, meta, true)
}

func NoChanges(headSHA string) string {
	return CommentMarker + "\n## quanto\n\nNo workflow execution changes as of commit " + inline(shortSHA(headSHA)) + ".\n"
}

func BelowThreshold(headSHA string) string {
	return CommentMarker + "\n## quanto\n\nNo changes that meet the comment threshold as of commit " + inline(shortSHA(headSHA)) + ". Details are in the quanto check run.\n"
}
