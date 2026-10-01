package report

import (
	"sort"
	"strconv"
	"strings"

	"github.com/BETAER-08/quanto/core/semdiff"
)

type Meta struct {
	HeadSHA      string `json:"head_sha"`
	SkippedFiles int    `json:"skipped_files"`
}

type Annotation struct {
	Path        string `json:"path"`
	StartLine   int    `json:"start_line"`
	EndLine     int    `json:"end_line"`
	StartColumn int    `json:"start_column"`
	EndColumn   int    `json:"end_column"`
	Level       string `json:"level"`
	Title       string `json:"title"`
	Message     string `json:"message"`
}

const CommentMarker = "<!-- quanto:summary -->"

const MaxBodyRunes = 60000

func Publishable(diffs []*semdiff.FileDiff) bool {
	for _, d := range diffs {
		if d == nil {
			continue
		}
		for _, f := range d.Findings {
			if f.Significance >= semdiff.High || commentKind(f.Kind) {
				return true
			}
		}
	}
	return false
}

func commentKind(kind string) bool {
	for _, prefix := range []string{"matrix.", "graph.", "estimate."} {
		if strings.HasPrefix(kind, prefix) {
			return true
		}
	}
	return false
}

func sortedDiffs(diffs []*semdiff.FileDiff) []*semdiff.FileDiff {
	out := make([]*semdiff.FileDiff, 0, len(diffs))
	for _, d := range diffs {
		if d != nil {
			out = append(out, d)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Path != out[j].Path {
			return out[i].Path < out[j].Path
		}
		return out[i].OldPath < out[j].OldPath
	})
	return out
}

func plural(n int, singular, pluralForm string) string {
	if n == 1 {
		return strconv.Itoa(n) + " " + singular
	}
	return strconv.Itoa(n) + " " + pluralForm
}

func shortSHA(sha string) string {
	r := []rune(sha)
	if len(r) > 7 {
		r = r[:7]
	}
	return string(r)
}

func absent(m semdiff.Metrics) bool {
	return m.JobsPerRun == ""
}

type metricRow struct {
	label  string
	before string
	after  string
}

func metricRows(d *semdiff.FileDiff) []metricRow {
	b, a := d.Before, d.After
	rows := []metricRow{
		{"Jobs per run", jobsCell(b), jobsCell(a)},
		{"Longest `needs` chain", intCell(b, b.Depth), intCell(a, a.Depth)},
		{"Max concurrent jobs", strCell(b, b.Width), strCell(a, a.Width)},
	}
	if b.RunnerMinutes != "" && a.RunnerMinutes != "" {
		rows = append(rows, metricRow{"Est. runner minutes per run", number(b.RunnerMinutes), number(a.RunnerMinutes)})
	}
	return rows
}

const absentCell = "—"

func jobsCell(m semdiff.Metrics) string {
	return strCell(m, m.JobsPerRun)
}

func strCell(m semdiff.Metrics, v string) string {
	if absent(m) {
		return absentCell
	}
	return number(v)
}

func intCell(m semdiff.Metrics, v int) string {
	if absent(m) {
		return absentCell
	}
	return strconv.Itoa(v)
}

func metricsChanged(d *semdiff.FileDiff) bool {
	return d.Before != d.After
}
