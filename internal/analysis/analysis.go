package analysis

import (
	"context"
	"errors"
	"sort"
	"strings"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/report"
	"github.com/BETAER-08/quanto/core/semdiff"
	"github.com/BETAER-08/quanto/core/source"
	"github.com/BETAER-08/quanto/internal/github"
)

const WorkflowDir = ".github/workflows/"

type Source interface {
	PullRequestFiles(ctx context.Context, owner, repo string, number int) ([]github.PullRequestFile, error)
	MergeBase(ctx context.Context, owner, repo, base, head string) (string, error)
	FileContent(ctx context.Context, owner, repo, path, ref string) ([]byte, bool, error)
}

type Request struct {
	Owner    string
	Repo     string
	Number   int
	BaseSHA  string
	HeadSHA  string
	MaxFiles int
}

type Result struct {
	MergeBase string
	Inputs    []semdiff.Input
	Meta      report.Meta
}

func IsWorkflowPath(p string) bool {
	name, ok := strings.CutPrefix(p, WorkflowDir)
	if !ok || strings.Contains(name, "/") {
		return false
	}
	for _, ext := range []string{".yml", ".yaml"} {
		if strings.HasSuffix(name, ext) && len(name) > len(ext) {
			return true
		}
	}
	return false
}

type plannedFile struct {
	path       string
	oldPath    string
	beforePath string
	afterPath  string
}

func planFiles(files []github.PullRequestFile) []plannedFile {
	var out []plannedFile
	for _, f := range files {
		newOK := IsWorkflowPath(f.Filename)
		oldOK := f.PreviousFilename != "" && IsWorkflowPath(f.PreviousFilename)
		if !newOK && !oldOK {
			continue
		}
		switch {
		case f.Status == "renamed" && newOK && oldOK:
			out = append(out, plannedFile{path: f.Filename, oldPath: f.PreviousFilename, beforePath: f.PreviousFilename, afterPath: f.Filename})
		case f.Status == "renamed" && oldOK:
			out = append(out, plannedFile{path: f.PreviousFilename, beforePath: f.PreviousFilename})
		case f.Status == "renamed", f.Status == "added", f.Status == "copied":
			out = append(out, plannedFile{path: f.Filename, afterPath: f.Filename})
		case f.Status == "removed":
			out = append(out, plannedFile{path: f.Filename, beforePath: f.Filename})
		default:
			out = append(out, plannedFile{path: f.Filename, beforePath: f.Filename, afterPath: f.Filename})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].path < out[j].path })
	return out
}

func Load(ctx context.Context, src Source, req Request) (*Result, error) {
	files, err := src.PullRequestFiles(ctx, req.Owner, req.Repo, req.Number)
	if err != nil {
		return nil, err
	}
	res := &Result{Meta: report.Meta{HeadSHA: req.HeadSHA}}
	planned := planFiles(files)
	if len(planned) == 0 {
		return res, nil
	}
	res.MergeBase, err = src.MergeBase(ctx, req.Owner, req.Repo, req.BaseSHA, req.HeadSHA)
	if err != nil {
		return nil, err
	}
	if req.MaxFiles > 0 && len(planned) > req.MaxFiles {
		res.Meta.SkippedFiles = len(planned) - req.MaxFiles
		planned = planned[:req.MaxFiles]
	}
	res.Inputs = make([]semdiff.Input, 0, len(planned))
	for _, f := range planned {
		in := semdiff.Input{Path: f.path, OldPath: f.oldPath}
		if f.beforePath != "" {
			if in.Before, in.BeforeErr, err = loadWorkflow(ctx, src, req, f.beforePath, res.MergeBase); err != nil {
				return nil, err
			}
		}
		if f.afterPath != "" {
			if in.After, in.AfterErr, err = loadWorkflow(ctx, src, req, f.afterPath, req.HeadSHA); err != nil {
				return nil, err
			}
		}
		res.Inputs = append(res.Inputs, in)
	}
	return res, nil
}

func loadWorkflow(ctx context.Context, src Source, req Request, path, ref string) (*model.Workflow, error, error) {
	content, found, err := src.FileContent(ctx, req.Owner, req.Repo, path, ref)
	if errors.Is(err, github.ErrFileTooLarge) {
		return nil, github.ErrFileTooLarge, nil
	}
	if err != nil {
		return nil, nil, err
	}
	if !found || len(content) == 0 {
		return nil, nil, nil
	}
	doc, err := source.Load(path, content)
	if err != nil {
		return nil, err, nil
	}
	w, _, err := model.Parse(doc)
	if err != nil {
		return nil, err, nil
	}
	return w, nil, nil
}

func (r *Result) Compare(opts semdiff.Options) []*semdiff.FileDiff {
	diffs := make([]*semdiff.FileDiff, 0, len(r.Inputs))
	for _, in := range r.Inputs {
		diffs = append(diffs, semdiff.Compare(in, opts))
	}
	return diffs
}

func HasFindings(diffs []*semdiff.FileDiff) bool {
	for _, d := range diffs {
		if d != nil && len(d.Findings) > 0 {
			return true
		}
	}
	return false
}

func CommentBody(diffs []*semdiff.FileDiff, meta report.Meta) (string, bool) {
	switch {
	case report.Publishable(diffs):
		return report.Markdown(diffs, meta), true
	case HasFindings(diffs):
		return report.BelowThreshold(meta.HeadSHA), false
	}
	return report.NoChanges(meta.HeadSHA), false
}
