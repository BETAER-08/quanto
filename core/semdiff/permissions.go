package semdiff

import (
	"slices"
	"sort"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

const (
	allReadAll  = "read-all"
	allWriteAll = "write-all"
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

func (c *comparer) permissionChanges(subject string, b, a model.PermissionSet) {
	switch {
	case !b.Declared && !a.Declared:
		return
	case b.Declared && !a.Declared:
		c.emit(Finding{Kind: kindPermissionsRemoved, Subject: subject, BasePos: b.Pos})
		return
	case !b.Declared && a.Declared:
		c.emit(Finding{Kind: kindPermissionsDeclared, Subject: subject, Pos: a.Pos})
		return
	}
	if a.All == allWriteAll && b.All != allWriteAll {
		c.emit(Finding{Kind: kindPermissionsWriteAll, Subject: subject, Pos: a.Pos})
		return
	}
	scopes := slices.Clone(officialScopes)
	for s := range b.Scopes {
		scopes = append(scopes, s)
	}
	for s := range a.Scopes {
		scopes = append(scopes, s)
	}
	sort.Strings(scopes)
	scopes = slices.Compact(scopes)
	for _, s := range scopes {
		bl, al := scopeLevel(b, s), scopeLevel(a, s)
		if bl == al {
			continue
		}
		kind := kindPermissionsBroadened
		if al < bl {
			kind = kindPermissionsNarrowed
		}
		c.emit(Finding{
			Kind:    kind,
			Subject: subject,
			Before:  levelName(bl),
			After:   levelName(al),
			Detail:  s,
			Pos:     scopePos(a, s),
		})
	}
}
