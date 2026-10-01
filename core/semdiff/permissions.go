package semdiff

import (
	"slices"
	"sort"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

const (
	allReadAll        = "read-all"
	allWriteAll       = "write-all"
	newJobLevel       = "none (new job)"
	repositoryDefault = "repository-default"
	workflowSubject   = "workflow"
)

var officialScopes = []string{
	"actions",
	"attestations",
	"checks",
	"contents",
	"deployments",
	"discussions",
	"id-token",
	"issues",
	"models",
	"packages",
	"pages",
	"pull-requests",
	"repository-projects",
	"security-events",
	"statuses",
}

func levelName(l model.Level) string {
	switch l {
	case model.LevelRead:
		return "read"
	case model.LevelWrite:
		return "write"
	}
	return "none"
}

func scopeLevel(ps model.PermissionSet, scope string) model.Level {
	switch ps.All {
	case allReadAll:
		return model.LevelRead
	case allWriteAll:
		return model.LevelWrite
	}
	if p, ok := ps.Scopes[scope]; ok {
		return p.Value
	}
	return model.LevelNone
}

func scopePos(ps model.PermissionSet, scope string) source.Position {
	if p, ok := ps.Scopes[scope]; ok && p.Pos.Valid() {
		return p.Pos
	}
	return ps.Pos
}

func effective(job, workflow model.PermissionSet) model.PermissionSet {
	if job.Declared {
		return job
	}
	return workflow
}

func jobSubject(id string) string {
	return "job `" + id + "`"
}

func scopeUnion(sets ...model.PermissionSet) []string {
	scopes := slices.Clone(officialScopes)
	for _, ps := range sets {
		for s := range ps.Scopes {
			scopes = append(scopes, s)
		}
	}
	sort.Strings(scopes)
	return slices.Compact(scopes)
}

type permItem struct {
	kind   string
	scope  string
	before string
	after  string
}

type permChange struct {
	item    permItem
	pos     source.Position
	basePos source.Position
}

func permDelta(b, a model.PermissionSet) []permChange {
	switch {
	case !b.Declared && !a.Declared:
		return nil
	case b.Declared && !a.Declared:
		return []permChange{{item: permItem{kind: kindPermissionsRemoved}, basePos: b.Pos}}
	case !b.Declared && a.Declared:
		return []permChange{{item: permItem{kind: kindPermissionsDeclared}, pos: a.Pos}}
	}
	if a.All == allWriteAll && b.All != allWriteAll {
		return []permChange{{item: permItem{kind: kindPermissionsWriteAll}, pos: a.Pos}}
	}
	var out []permChange
	for _, s := range scopeUnion(b, a) {
		bl, al := scopeLevel(b, s), scopeLevel(a, s)
		if bl == al {
			continue
		}
		kind := kindPermissionsBroadened
		if al < bl {
			kind = kindPermissionsNarrowed
		}
		out = append(out, permChange{
			item: permItem{kind: kind, scope: s, before: levelName(bl), after: levelName(al)},
			pos:  scopePos(a, s),
		})
	}
	return out
}

func noDeclarations(w *model.Workflow) bool {
	if w.Permissions.Declared {
		return false
	}
	for _, j := range w.Jobs {
		if j != nil && j.Permissions.Declared {
			return false
		}
	}
	return true
}

func (c *comparer) emitPerm(subject string, ch permChange) {
	f := Finding{
		Kind:    ch.item.kind,
		Subject: subject,
		Before:  ch.item.before,
		After:   ch.item.after,
		Detail:  ch.item.scope,
		Pos:     ch.pos,
		BasePos: ch.basePos,
	}
	if ch.item.kind == kindPermissionsRemoved && noDeclarations(c.after.wf) {
		f.Detail = repositoryDefault
	}
	c.emit(f)
}

func (c *comparer) workflowPermissionDelta() []permChange {
	return permDelta(c.before.wf.Permissions, c.after.wf.Permissions)
}

func (c *comparer) workflowPermissionChanges() {
	for _, ch := range c.workflowPermissionDelta() {
		c.emitPerm(workflowSubject, ch)
	}
}

func (c *comparer) jobPermissionChanges(bj, aj *model.Job) {
	wf := make(map[permItem]bool)
	for _, ch := range c.workflowPermissionDelta() {
		wf[ch.item] = true
	}
	eb := effective(bj.Permissions, c.before.wf.Permissions)
	ea := effective(aj.Permissions, c.after.wf.Permissions)
	for _, ch := range permDelta(eb, ea) {
		if wf[ch.item] {
			continue
		}
		c.emitPerm(jobSubject(aj.ID), ch)
	}
}

func (c *comparer) addedJobPermissions(aj *model.Job) {
	ea := aj.Permissions
	if !ea.Declared {
		return
	}
	subject := jobSubject(aj.ID)
	if ea.All == allWriteAll {
		c.emit(Finding{Kind: kindPermissionsWriteAll, Subject: subject, Before: newJobLevel, Pos: ea.Pos})
		return
	}
	for _, s := range scopeUnion(ea) {
		if scopeLevel(ea, s) != model.LevelWrite {
			continue
		}
		c.emit(Finding{
			Kind:    kindPermissionsBroadened,
			Subject: subject,
			Before:  newJobLevel,
			After:   levelName(model.LevelWrite),
			Detail:  s,
			Pos:     scopePos(ea, s),
		})
	}
}
