package analysis

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/BETAER-08/quanto/core/report"
	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/internal/github"
)

const (
	fixtureDir = "../../testdata/golden/semdiff/"
	ciPath     = ".github/workflows/ci.yml"
	baseSHA    = "2222222222222222222222222222222222222222"
	headSHA    = "1111111111111111111111111111111111111111"
	mergeSHA   = "3333333333333333333333333333333333333333"
)

type fakeSource struct {
	files       []github.PullRequestFile
	filesErr    error
	mergeErr    error
	contents    map[string][]byte
	tooLarge    map[string]bool
	contentErr  error
	mergeCalls  int
	contentRefs []string
}

func (f *fakeSource) PullRequestFiles(ctx context.Context, owner, repo string, number int) ([]github.PullRequestFile, error) {
	return f.files, f.filesErr
}

func (f *fakeSource) MergeBase(ctx context.Context, owner, repo, base, head string) (string, error) {
	f.mergeCalls++
	if f.mergeErr != nil {
		return "", f.mergeErr
	}
	if base != baseSHA || head != headSHA {
		return "", errors.New("unexpected merge base arguments")
	}
	return mergeSHA, nil
}

func (f *fakeSource) FileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, bool, error) {
	key := ref + ":" + path
	f.contentRefs = append(f.contentRefs, key)
	if f.contentErr != nil {
		return nil, false, f.contentErr
	}
	if f.tooLarge[key] {
		return nil, true, github.ErrFileTooLarge
	}
	data, ok := f.contents[key]
	return data, ok, nil
}

func readFixture(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(fixtureDir + name)
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	return data
}

func request() Request {
	return Request{Owner: "o", Repo: "r", Number: 3, BaseSHA: baseSHA, HeadSHA: headSHA, MaxFiles: 50}
}

func TestPlanFiles(t *testing.T) {
	files := []github.PullRequestFile{
		{Filename: "README.md", Status: "modified"},
		{Filename: ".github/workflows/z.yml", Status: "modified"},
		{Filename: ".github/workflows/new.yml", Status: "added"},
		{Filename: ".github/workflows/old.yml", Status: "removed"},
		{Filename: ".github/workflows/b.yml", PreviousFilename: ".github/workflows/a.yml", Status: "renamed"},
		{Filename: ".github/workflows/in.yml", PreviousFilename: "ci/in.yml", Status: "renamed"},
		{Filename: "ci/out.yml", PreviousFilename: ".github/workflows/out.yml", Status: "renamed"},
		{Filename: ".github/workflows/copy.yaml", PreviousFilename: ".github/workflows/z.yml", Status: "copied"},
		{Filename: ".github/workflows/nested/x.yml", Status: "added"},
	}
	want := []plannedFile{
		{path: ".github/workflows/b.yml", oldPath: ".github/workflows/a.yml", beforePath: ".github/workflows/a.yml", afterPath: ".github/workflows/b.yml"},
		{path: ".github/workflows/copy.yaml", afterPath: ".github/workflows/copy.yaml"},
		{path: ".github/workflows/in.yml", afterPath: ".github/workflows/in.yml"},
		{path: ".github/workflows/new.yml", afterPath: ".github/workflows/new.yml"},
		{path: ".github/workflows/old.yml", beforePath: ".github/workflows/old.yml"},
		{path: ".github/workflows/out.yml", beforePath: ".github/workflows/out.yml"},
		{path: ".github/workflows/z.yml", beforePath: ".github/workflows/z.yml", afterPath: ".github/workflows/z.yml"},
	}
	got := planFiles(files)
	if len(got) != len(want) {
		t.Fatalf("planFiles = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("planFiles[%d] = %+v, want %+v", i, got[i], want[i])
		}
	}
}

func TestIsWorkflowPath(t *testing.T) {
	tests := map[string]bool{
		".github/workflows/ci.yml":       true,
		".github/workflows/ci.yaml":      true,
		".github/workflows/.yml":         false,
		".github/workflows/a/ci.yml":     false,
		".github/workflows/ci.yml.bak":   false,
		"github/workflows/ci.yml":        false,
		".github/workflows/README.md":    false,
		"x/.github/workflows/ci.yml":     false,
		".github/workflows/a.b.yml":      true,
		".github/workflows/ci.YML":       false,
		".github/workflows/한글.yml":       true,
		".github/workflows/with space.y": false,
	}
	for in, want := range tests {
		if got := IsWorkflowPath(in); got != want {
			t.Errorf("IsWorkflowPath(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestLoadNoWorkflowFiles(t *testing.T) {
	src := &fakeSource{files: []github.PullRequestFile{{Filename: "README.md", Status: "modified"}}}
	res, err := Load(context.Background(), src, request())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.Inputs) != 0 || res.MergeBase != "" || src.mergeCalls != 0 {
		t.Fatalf("result = %+v, merge calls = %d", res, src.mergeCalls)
	}
	if res.Meta.HeadSHA != headSHA {
		t.Fatalf("meta = %+v", res.Meta)
	}
}

func TestLoadReadsMergeBaseAndHead(t *testing.T) {
	src := &fakeSource{
		files: []github.PullRequestFile{{Filename: ciPath, Status: "modified"}},
		contents: map[string][]byte{
			mergeSHA + ":" + ciPath: readFixture(t, "matrix-axis-added/before.yml"),
			baseSHA + ":" + ciPath:  readFixture(t, "matrix-axis-added/after.yml"),
			headSHA + ":" + ciPath:  readFixture(t, "matrix-axis-added/after.yml"),
		},
	}
	res, err := Load(context.Background(), src, request())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if res.MergeBase != mergeSHA {
		t.Fatalf("merge base = %q", res.MergeBase)
	}
	want := []string{mergeSHA + ":" + ciPath, headSHA + ":" + ciPath}
	if strings.Join(src.contentRefs, "|") != strings.Join(want, "|") {
		t.Fatalf("content refs = %v", src.contentRefs)
	}
	diffs := res.Compare(semdiff.Options{})
	if len(diffs) != 1 || diffs[0].Status != semdiff.StatusModified {
		t.Fatalf("diffs = %+v", diffs)
	}
	kinds := []string{}
	for _, f := range diffs[0].Findings {
		kinds = append(kinds, f.Kind)
	}
	if strings.Join(kinds, ",") != "matrix.count_changed" {
		t.Fatalf("kinds = %v", kinds)
	}
}

func TestLoadSidesAndErrors(t *testing.T) {
	valid := readFixture(t, "identical/before.yml")
	src := &fakeSource{
		files: []github.PullRequestFile{
			{Filename: ".github/workflows/added.yml", Status: "added"},
			{Filename: ".github/workflows/removed.yml", Status: "removed"},
			{Filename: ".github/workflows/big.yml", Status: "modified"},
			{Filename: ".github/workflows/broken.yml", Status: "modified"},
			{Filename: ".github/workflows/new.yml", PreviousFilename: ".github/workflows/old.yml", Status: "renamed"},
		},
		contents: map[string][]byte{
			headSHA + ":.github/workflows/added.yml":    valid,
			mergeSHA + ":.github/workflows/removed.yml": valid,
			mergeSHA + ":.github/workflows/big.yml":     valid,
			mergeSHA + ":.github/workflows/broken.yml":  valid,
			headSHA + ":.github/workflows/broken.yml":   []byte("on: [push\n"),
			mergeSHA + ":.github/workflows/old.yml":     valid,
			headSHA + ":.github/workflows/new.yml":      valid,
		},
		tooLarge: map[string]bool{headSHA + ":.github/workflows/big.yml": true},
	}
	res, err := Load(context.Background(), src, request())
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.Inputs) != 5 {
		t.Fatalf("inputs = %d", len(res.Inputs))
	}
	byPath := map[string]semdiff.Input{}
	for _, in := range res.Inputs {
		byPath[in.Path] = in
	}
	if in := byPath[".github/workflows/added.yml"]; in.Before != nil || in.After == nil {
		t.Errorf("added = %+v", in)
	}
	if in := byPath[".github/workflows/removed.yml"]; in.Before == nil || in.After != nil {
		t.Errorf("removed = %+v", in)
	}
	if in := byPath[".github/workflows/big.yml"]; !errors.Is(in.AfterErr, github.ErrFileTooLarge) {
		t.Errorf("big = %+v", in)
	}
	if in := byPath[".github/workflows/broken.yml"]; in.AfterErr == nil || in.Before == nil {
		t.Errorf("broken = %+v", in)
	}
	if in := byPath[".github/workflows/new.yml"]; in.OldPath != ".github/workflows/old.yml" || in.Before == nil || in.After == nil {
		t.Errorf("renamed = %+v", in)
	}
}

func TestLoadMaxFiles(t *testing.T) {
	src := &fakeSource{files: []github.PullRequestFile{
		{Filename: ".github/workflows/c.yml", Status: "added"},
		{Filename: ".github/workflows/a.yml", Status: "added"},
		{Filename: ".github/workflows/b.yml", Status: "added"},
	}}
	req := request()
	req.MaxFiles = 2
	res, err := Load(context.Background(), src, req)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(res.Inputs) != 2 || res.Inputs[0].Path != ".github/workflows/a.yml" || res.Inputs[1].Path != ".github/workflows/b.yml" {
		t.Fatalf("inputs = %+v", res.Inputs)
	}
	if res.Meta.SkippedFiles != 1 {
		t.Fatalf("skipped = %d", res.Meta.SkippedFiles)
	}
}

func TestLoadPropagatesSourceErrors(t *testing.T) {
	boom := errors.New("boom")
	files := []github.PullRequestFile{{Filename: ciPath, Status: "modified"}}
	tests := map[string]*fakeSource{
		"files":   {filesErr: boom},
		"merge":   {files: files, mergeErr: boom},
		"content": {files: files, contentErr: boom},
	}
	for name, src := range tests {
		if _, err := Load(context.Background(), src, request()); !errors.Is(err, boom) {
			t.Errorf("%s: err = %v", name, err)
		}
	}
}

func TestCommentBody(t *testing.T) {
	meta := report.Meta{HeadSHA: headSHA}
	high := []*semdiff.FileDiff{{Path: ciPath, Findings: []semdiff.Finding{{Kind: "permissions.write_all", Significance: semdiff.High, Subject: "workflow"}}}}
	normal := []*semdiff.FileDiff{{Path: ciPath, Findings: []semdiff.Finding{{Kind: "trigger.added", Significance: semdiff.Normal, Subject: "push"}}}}
	none := []*semdiff.FileDiff{{Path: ciPath, Findings: []semdiff.Finding{}}}
	body, ok := CommentBody(high, meta, report.DetailsCheckRun)
	if !ok || body != report.Markdown(high, meta) {
		t.Errorf("high = %v %q", ok, body)
	}
	body, ok = CommentBody(normal, meta, report.DetailsCheckRun)
	if ok || body != report.BelowThreshold(headSHA, report.DetailsCheckRun) {
		t.Errorf("normal check run = %v %q", ok, body)
	}
	body, ok = CommentBody(normal, meta, report.DetailsJobSummary)
	if ok || body != report.BelowThreshold(headSHA, report.DetailsJobSummary) {
		t.Errorf("normal job summary = %v %q", ok, body)
	}
	body, ok = CommentBody(none, meta, report.DetailsCheckRun)
	if ok || body != report.NoChanges(headSHA) {
		t.Errorf("none = %v %q", ok, body)
	}
}
