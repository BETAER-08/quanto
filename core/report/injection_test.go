package report

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/BETAER-08/quanto/core/semdiff"
)

var injectionDir = filepath.Join("..", "..", "testdata", "golden", "report", "injection")

var injectionPayloads = []string{
	"x\n\n## Approved",
	"",
	"![](https://evil.example/p.png)",
	"@org/admins",
	"```",
	"<details>",
	"<!-- quanto:summary -->",
	"`lead and trail`",
	"[link](https://evil.example)",
	strings.Repeat("a", 70) + "\r\n\t" + strings.Repeat("`", 3) + "<details>" + strings.Repeat("b", 20),
}

func injectedDiff() *semdiff.FileDiff {
	d := &semdiff.FileDiff{
		Path:    ".github/workflows/x`y<details>@org/admins.yml",
		OldPath: ".github/workflows/old\n## Approved.yml",
		Status:  semdiff.StatusRenamed,
		Before:  semdiff.Metrics{JobsPerRun: "1", Depth: 1, Width: "1"},
		After:   semdiff.Metrics{JobsPerRun: "2", Depth: 1, Width: "2"},
	}
	n := len(injectionPayloads)
	for i, kind := range semdiff.Kinds() {
		d.Findings = append(d.Findings, semdiff.Finding{
			Kind:         kind,
			Significance: semdiff.Normal,
			Subject:      injectionPayloads[i%n],
			Before:       injectionPayloads[(i+1)%n],
			After:        injectionPayloads[(i+2)%n],
			Detail:       injectionPayloads[(i+3)%n],
			Pos:          pos(i+1, 1, i+1, 5),
		})
	}
	d.Findings = append(d.Findings,
		semdiff.Finding{Kind: "permissions.broadened", Significance: semdiff.High, Subject: "job `" + injectionPayloads[0] + "`", Before: "read", After: "write", Detail: injectionPayloads[5], Pos: pos(1, 1, 1, 2)},
		semdiff.Finding{Kind: "unknown.kind\n## Approved", Significance: semdiff.Normal, Subject: injectionPayloads[3], Pos: pos(2, 1, 2, 2)},
	)
	return d
}

func injectionDiffs(t *testing.T) []*semdiff.FileDiff {
	t.Helper()
	in := semdiff.Input{Path: goldenPath}
	if w, err, ok := parseSide(t, filepath.Join(injectionDir, "before.yml")); ok {
		in.Before, in.BeforeErr = w, err
	}
	if w, err, ok := parseSide(t, filepath.Join(injectionDir, "after.yml")); ok {
		in.After, in.AfterErr = w, err
	}
	fromYAML := semdiff.Compare(in, semdiff.Options{})
	if len(fromYAML.Findings) == 0 || fromYAML.Error != "" {
		t.Fatalf("injection workflow produced no findings: %+v", fromYAML)
	}
	return []*semdiff.FileDiff{fromYAML, injectedDiff()}
}

func outsideCodeSpans(s string) string {
	var b strings.Builder
	runLen := func(i int) int {
		n := 0
		for i+n < len(s) && s[i+n] == '`' {
			n++
		}
		return n
	}
	for i := 0; i < len(s); {
		if s[i] != '`' {
			b.WriteByte(s[i])
			i++
			continue
		}
		n := runLen(i)
		closed := -1
		for j := i + n; j < len(s); {
			if s[j] != '`' {
				j++
				continue
			}
			m := runLen(j)
			if m == n {
				closed = j
				break
			}
			j += m
		}
		if closed < 0 {
			b.WriteString(s[i : i+n])
			i += n
			continue
		}
		b.WriteString("\x00")
		i = closed + n
	}
	return b.String()
}

func TestOutsideCodeSpans(t *testing.T) {
	tests := []struct{ in, want string }{
		{"a `b` c", "a \x00 c"},
		{"`` a`b `` c", "\x00 c"},
		{"`` x", "`` x"},
		{"`a`` b", "`a`` b"},
	}
	for _, tt := range tests {
		if got := outsideCodeSpans(tt.in); got != tt.want {
			t.Errorf("outsideCodeSpans(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestInline(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain", "`plain`"},
		{"", "` `"},
		{"a`b", "``a`b``"},
		{"a``b`c", "```a``b`c```"},
		{"`x", "`` `x ``"},
		{"x`", "`` x` ``"},
		{"```", "```` ``` ````"},
		{"a\nb\r\tc\x00d\u0085e", "`a b  c d e`"},
		{strings.Repeat("가", 80), "`" + strings.Repeat("가", 80) + "`"},
		{strings.Repeat("가", 81), "`" + strings.Repeat("가", 79) + "…`"},
		{strings.Repeat("a", 79) + "``x", "`" + strings.Repeat("a", 79) + "…`"},
		{strings.Repeat("a", 78) + "`xyz", "``" + strings.Repeat("a", 78) + "`…``"},
		{strings.Repeat("a", 78) + "`x", "``" + strings.Repeat("a", 78) + "`x``"},
		{"`" + strings.Repeat("a", 90), "`` `" + strings.Repeat("a", 78) + "… ``"},
	}
	for _, tt := range tests {
		if got := inline(tt.in); got != tt.want {
			t.Errorf("inline(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

var forbiddenOutside = []string{"Approved", "evil", "@", "<details", "![", "[", "admins", "quanto:summary", "`", "\r", "\t"}

func assertNoMarkup(t *testing.T, name, out string, markers int) {
	t.Helper()
	outside := outsideCodeSpans(out)
	if strings.Count(outside, CommentMarker) != markers {
		t.Errorf("%s: marker appears outside code spans %d times, want %d", name, strings.Count(outside, CommentMarker), markers)
	}
	rest := strings.ReplaceAll(outside, CommentMarker, "")
	for _, f := range forbiddenOutside {
		if strings.Contains(rest, f) {
			t.Errorf("%s: %q outside code spans:\n%s", name, f, rest)
		}
	}
	rest = strings.ReplaceAll(strings.ReplaceAll(rest, "<sub>", ""), "</sub>", "")
	if strings.Contains(rest, "<") {
		t.Errorf("%s: raw HTML outside code spans:\n%s", name, rest)
	}
	for _, line := range strings.Split(rest, "\n") {
		if strings.HasPrefix(line, "#") && line != "## quanto" && !strings.HasPrefix(line, "### \x00") {
			t.Errorf("%s: unexpected heading %q", name, line)
		}
	}
}

func TestInjectionGolden(t *testing.T) {
	diffs := injectionDiffs(t)
	meta := Meta{HeadSHA: goldenSHA}
	md := Markdown(diffs, meta)
	title, summary := CheckSummary(diffs, meta)
	txt := Text(diffs)
	annotations := Annotations(diffs)
	ann, err := json.MarshalIndent(annotations, "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	ann = append(ann, '\n')
	checkGolden(t, filepath.Join(injectionDir, "expected.md"), []byte(md))
	checkGolden(t, filepath.Join(injectionDir, "expected-summary.md"), []byte(title+"\n\n"+summary))
	checkGolden(t, filepath.Join(injectionDir, "expected.txt"), []byte(txt))
	checkGolden(t, filepath.Join(injectionDir, "expected-annotations.json"), ann)

	assertNoMarkup(t, "markdown", md, 1)
	assertNoMarkup(t, "summary", summary, 0)
	plain := txt
	for _, sig := range []string{"[low]", "[normal]", "[high]"} {
		plain = strings.ReplaceAll(plain, sig, "")
	}
	assertNoMarkup(t, "text", plain, 0)
	assertNoMarkup(t, "no-changes", NoChanges("`x<details>@org/admins\n## Approved"), 1)
	assertNoMarkup(t, "below-threshold", BelowThreshold("`x<details>@org/admins\n## Approved", DetailsCheckRun), 1)
	assertNoMarkup(t, "below-threshold-job-summary", BelowThreshold("`x<details>@org/admins\n## Approved", DetailsJobSummary), 1)
	for _, line := range strings.Split(strings.TrimSuffix(txt, "\n"), "\n") {
		switch {
		case line == "":
		case strings.HasPrefix(line, "`"):
		case strings.HasPrefix(line, "  - ["):
		case strings.HasPrefix(line, "  ") && strings.Contains(line, " → "):
		default:
			t.Errorf("text line not produced by renderer: %q", line)
		}
	}
	findings := 0
	for _, d := range diffs {
		for _, f := range d.Findings {
			if f.Significance >= semdiff.Normal {
				findings++
			}
		}
	}
	if got := strings.Count(md, "\n- "); got != findings {
		t.Errorf("markdown has %d list items, want %d", got, findings)
	}
	for _, a := range annotations {
		if strings.ContainsAny(a.Message, "\r\n\t") {
			t.Errorf("annotation message contains control characters: %q", a.Message)
		}
		assertNoMarkup(t, "annotation "+a.Title, a.Message, 0)
		known := false
		for _, k := range semdiff.Kinds() {
			known = known || a.Title == k
		}
		if !known && a.Title != inline("unknown.kind\n## Approved") {
			t.Errorf("unexpected annotation title %q", a.Title)
		}
	}
}
