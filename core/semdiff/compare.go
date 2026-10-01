package semdiff

import (
	"sort"

	"github.com/BETAER-08/quanto/core/source"
)

const defaultMinSamples = 5

type comparer struct {
	opts   Options
	diff   *FileDiff
	before *side
	after  *side
}

func (c *comparer) emit(f Finding) {
	c.emitSig(f, defaultSignificance(f.Kind))
}

func (c *comparer) emitSig(f Finding, sig Significance) {
	f.Significance = sig
	c.diff.Findings = append(c.diff.Findings, f)
}

func Compare(in Input, opts Options) *FileDiff {
	if opts.MinSamples <= 0 {
		opts.MinSamples = defaultMinSamples
	}
	d := &FileDiff{Path: in.Path, OldPath: in.OldPath, Findings: []Finding{}}
	c := &comparer{opts: opts, diff: d}
	beforePath := in.Path
	if in.OldPath != "" {
		beforePath = in.OldPath
	}
	if in.BeforeErr == nil && in.Before != nil {
		c.before = analyze(in.Before, beforePath, opts)
		d.Before = c.before.metrics
	}
	if in.AfterErr == nil && in.After != nil {
		c.after = analyze(in.After, in.Path, opts)
		d.After = c.after.metrics
	}
	switch {
	case in.BeforeErr != nil || in.AfterErr != nil:
		d.Status = StatusUnanalyzable
		d.Error = errorText(in.BeforeErr, in.AfterErr)
		c.emit(Finding{Kind: kindWorkflowUnanalyzable, Detail: d.Error})
	case c.before == nil && c.after == nil:
		d.Status = StatusUnanalyzable
		d.Error = "no workflow content on either side"
		c.emit(Finding{Kind: kindWorkflowUnanalyzable, Detail: d.Error})
	case c.before == nil:
		d.Status = StatusAdded
		c.emit(Finding{Kind: kindWorkflowAdded})
	case c.after == nil:
		d.Status = StatusRemoved
		c.emit(Finding{Kind: kindWorkflowRemoved})
	default:
		d.Status = StatusModified
		if in.OldPath != "" && in.OldPath != in.Path {
			d.Status = StatusRenamed
			c.emit(Finding{Kind: kindWorkflowRenamed, Before: in.OldPath, After: in.Path})
		}
		c.compareWorkflows()
	}
	sortFindings(d.Findings)
	return d
}

func errorText(before, after error) string {
	switch {
	case before != nil && after != nil:
		return "base: " + before.Error() + "; head: " + after.Error()
	case before != nil:
		return before.Error()
	case after != nil:
		return after.Error()
	}
	return ""
}

func (c *comparer) compareWorkflows() {
	c.triggerChanges()
	c.permissionChanges("workflow", c.before.wf.Permissions, c.after.wf.Permissions)
	c.jobChanges()
	c.actionChanges()
	c.secretChanges()
	c.graphChanges()
	c.estimateChange()
}

func lessPos(a, b source.Position) (bool, bool) {
	if a.Line != b.Line {
		return a.Line < b.Line, true
	}
	if a.Column != b.Column {
		return a.Column < b.Column, true
	}
	return false, false
}

func sortFindings(fs []Finding) {
	sort.SliceStable(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Significance != b.Significance {
			return a.Significance > b.Significance
		}
		if ra, rb := rankOf(a.Kind), rankOf(b.Kind); ra != rb {
			return ra < rb
		}
		if a.Kind != b.Kind {
			return a.Kind < b.Kind
		}
		if a.Subject != b.Subject {
			return a.Subject < b.Subject
		}
		if a.Before != b.Before {
			return a.Before < b.Before
		}
		if a.After != b.After {
			return a.After < b.After
		}
		if a.Detail != b.Detail {
			return a.Detail < b.Detail
		}
		if less, ok := lessPos(a.Pos, b.Pos); ok {
			return less
		}
		less, _ := lessPos(a.BasePos, b.BasePos)
		return less
	})
}
