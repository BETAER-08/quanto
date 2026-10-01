package semdiff

import (
	"sort"
	"strconv"
	"strings"

	"github.com/BETAER-08/quanto/core/expr"
	"github.com/BETAER-08/quanto/core/matrix"
	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

func JobKey(j *model.Job) string {
	if j == nil {
		return ""
	}
	key := j.ID
	if name := j.Name.Value; name != "" && !expr.IsDynamic(name) {
		key = name
	}
	return stripMatrixSuffix(key)
}

func NormalizeRunJobName(name string) (string, bool) {
	if strings.Contains(name, " / ") {
		return "", false
	}
	out := stripMatrixSuffix(name)
	if out == "" {
		return "", false
	}
	return out, true
}

func stripMatrixSuffix(name string) string {
	if !strings.HasSuffix(name, ")") {
		return name
	}
	depth := 0
	for i := len(name) - 1; i >= 0; i-- {
		switch name[i] {
		case ')':
			depth++
		case '(':
			depth--
			if depth == 0 {
				if i > 0 && name[i-1] == ' ' {
					return name[:i-1]
				}
				return name
			}
		}
	}
	return name
}

type jobPair struct {
	before *jobInfo
	after  *jobInfo
}

type renameCandidate struct {
	before string
	after  string
	inter  int64
	union  int64
}

func stepSignature(j *model.Job) map[string]int {
	sig := make(map[string]int)
	for _, s := range j.Steps {
		if s == nil {
			continue
		}
		switch {
		case s.Uses != nil:
			sig["uses:"+s.Uses.Identity()]++
		case s.Run != nil:
			sig["run:"+firstLine(s.Run.Value)]++
		}
	}
	return sig
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(line); t != "" {
			return t
		}
	}
	return ""
}

func similarity(b, a *model.Job) (int64, int64) {
	if len(b.Steps) == 0 || len(a.Steps) == 0 {
		if len(b.Steps) == 0 && len(a.Steps) == 0 && b.Uses != nil && a.Uses != nil && b.Uses.Raw == a.Uses.Raw {
			return 1, 1
		}
		return 0, 1
	}
	bs, as := stepSignature(b), stepSignature(a)
	var inter, union int64
	for k, bn := range bs {
		an := as[k]
		inter += int64(min(bn, an))
		union += int64(max(bn, an))
	}
	for k, an := range as {
		if _, ok := bs[k]; !ok {
			union += int64(an)
		}
	}
	if union == 0 {
		return 0, 1
	}
	return inter, union
}

func (c *comparer) matchJobs() ([]jobPair, []string, []string, map[string]string) {
	b, a := c.before, c.after
	var removed, added []string
	for _, id := range b.order {
		if _, ok := a.jobs[id]; !ok {
			removed = append(removed, id)
		}
	}
	for _, id := range a.order {
		if _, ok := b.jobs[id]; !ok {
			added = append(added, id)
		}
	}
	var cands []renameCandidate
	for _, r := range removed {
		for _, ad := range added {
			inter, union := similarity(b.jobs[r].job, a.jobs[ad].job)
			if inter*10 >= union*7 && inter > 0 {
				cands = append(cands, renameCandidate{before: r, after: ad, inter: inter, union: union})
			}
		}
	}
	sort.SliceStable(cands, func(i, j int) bool {
		x, y := cands[i], cands[j]
		if l, r := x.inter*y.union, y.inter*x.union; l != r {
			return l > r
		}
		if x.before != y.before {
			return x.before < y.before
		}
		return x.after < y.after
	})
	renamedTo := make(map[string]string)
	usedBefore := make(map[string]bool)
	for _, cand := range cands {
		if usedBefore[cand.before] {
			continue
		}
		if _, used := renamedTo[cand.after]; used {
			continue
		}
		usedBefore[cand.before] = true
		renamedTo[cand.after] = cand.before
	}
	var pairs []jobPair
	var onlyAfter, onlyBefore []string
	for _, id := range a.order {
		if bi, ok := b.jobs[id]; ok {
			pairs = append(pairs, jobPair{before: bi, after: a.jobs[id]})
			continue
		}
		if from, ok := renamedTo[id]; ok {
			pairs = append(pairs, jobPair{before: b.jobs[from], after: a.jobs[id]})
			continue
		}
		onlyAfter = append(onlyAfter, id)
	}
	for _, id := range removed {
		if !usedBefore[id] {
			onlyBefore = append(onlyBefore, id)
		}
	}
	return pairs, onlyAfter, onlyBefore, renamedTo
}

func (c *comparer) jobChanges() {
	pairs, added, removed, renamedTo := c.matchJobs()
	for _, p := range pairs {
		aj, bj := p.after.job, p.before.job
		if from, ok := renamedTo[aj.ID]; ok {
			c.emit(Finding{Kind: kindJobRenamed, Subject: aj.ID, Before: from, After: aj.ID, Pos: aj.Pos, BasePos: bj.Pos})
		}
		c.pairChanges(p)
	}
	for _, id := range added {
		info := c.after.jobs[id]
		c.emit(Finding{Kind: kindJobAdded, Subject: id, Pos: info.job.Pos})
		c.matrixChange(nil, info)
		c.inheritChange(nil, info.job)
	}
	for _, id := range removed {
		c.emit(Finding{Kind: kindJobRemoved, Subject: id, BasePos: c.before.jobs[id].job.Pos})
	}
}

func orJobPos(p source.Position, j *model.Job) source.Position {
	if p.Valid() {
		return p
	}
	return j.Pos
}

func (c *comparer) pairChanges(p jobPair) {
	aj, bj := p.after.job, p.before.job
	if br, ar := formatRunner(bj.RunsOn), formatRunner(aj.RunsOn); br != ar {
		c.emit(Finding{Kind: kindJobRunnerChanged, Subject: aj.ID, Before: br, After: ar, Pos: orJobPos(aj.RunsOn.Pos, aj)})
	}
	if bt, at := formatTimeout(bj), formatTimeout(aj); bt != at {
		pos := aj.Pos
		if aj.TimeoutMinutes != nil {
			pos = orJobPos(aj.TimeoutMinutes.Pos, aj)
		}
		c.emit(Finding{Kind: kindJobTimeoutChanged, Subject: aj.ID, Before: bt, After: at, Pos: pos})
	}
	if bc, ac := formatConcurrency(bj.Concurrency), formatConcurrency(aj.Concurrency); bc != ac {
		pos := aj.Pos
		if aj.Concurrency != nil {
			pos = orJobPos(aj.Concurrency.Pos, aj)
		}
		c.emit(Finding{Kind: kindJobConcurrencyChanged, Subject: aj.ID, Before: bc, After: ac, Pos: pos})
	}
	c.matrixChange(p.before, p.after)
	c.permissionChanges("job `"+aj.ID+"`", bj.Permissions, aj.Permissions)
	c.inheritChange(bj, aj)
}

func formatRunner(rs model.RunnerSpec) string {
	labels := sortedUnique(rs.Labels)
	text := strings.Join(labels, ", ")
	if rs.Group != "" {
		if text == "" {
			return "group " + rs.Group
		}
		return "group " + rs.Group + ": " + text
	}
	if text == "" {
		return noneText
	}
	return text
}

func formatTimeout(j *model.Job) string {
	if j.TimeoutMinutes == nil {
		return noneText
	}
	return j.TimeoutMinutes.Value
}

func formatConcurrency(cc *model.Concurrency) string {
	if cc == nil {
		return noneText
	}
	text := cc.Group
	if cc.CancelInProgress != "" {
		text += " (cancel-in-progress: " + cc.CancelInProgress + ")"
	}
	if text == "" {
		return noneText
	}
	return text
}

func orMatrixPos(info *jobInfo) source.Position {
	return orJobPos(info.matrixPos, info.job)
}

func (c *comparer) matrixChange(before, after *jobInfo) {
	if after.invalid || (before != nil && before.invalid) {
		return
	}
	if after.dynamic {
		if before == nil || !before.dynamic {
			c.emit(Finding{Kind: kindMatrixDynamic, Subject: after.job.ID, Pos: orMatrixPos(after)})
		}
		return
	}
	newlyOver := after.count > matrix.GitHubJobLimit && (before == nil || before.dynamic || before.count <= matrix.GitHubJobLimit)
	if newlyOver {
		f := Finding{Kind: kindMatrixOverLimit, Subject: after.job.ID, After: strconv.Itoa(after.count), Pos: orMatrixPos(after)}
		if before != nil {
			f.Before = before.countText()
		}
		c.emit(f)
		return
	}
	if before == nil {
		return
	}
	if !before.dynamic && before.count == after.count {
		return
	}
	sig := Normal
	if !before.dynamic {
		if before.count == 0 {
			sig = High
		} else if ratio := float64(after.count) / float64(before.count); ratio >= 2 || ratio <= 0.5 {
			sig = High
		}
	}
	c.emitSig(Finding{
		Kind:    kindMatrixCountChanged,
		Subject: after.job.ID,
		Before:  before.countText(),
		After:   after.countText(),
		Pos:     orMatrixPos(after),
	}, sig)
}

func (c *comparer) inheritChange(before, after *model.Job) {
	if !after.SecretsInherit || (before != nil && before.SecretsInherit) {
		return
	}
	f := Finding{Kind: kindSecretsInheritAdded, Subject: after.ID, Pos: after.Pos}
	if after.Uses != nil {
		f.After = after.Uses.Raw
	}
	c.emit(f)
}
