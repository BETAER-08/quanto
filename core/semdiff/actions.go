package semdiff

import (
	"sort"
	"strings"

	"github.com/BETAER-08/quanto/core/model"
	"github.com/BETAER-08/quanto/core/source"
)

type useSite struct {
	ref string
	pos source.Position
}

type usage struct {
	refs       map[string]model.RefKind
	sites      []useSite
	thirdParty bool
}

func reusableIdentity(r *model.ReusableRef) string {
	if r.Local {
		return r.Path
	}
	id := strings.ToLower(r.Owner)
	if r.Repo != "" {
		id += "/" + strings.ToLower(r.Repo)
	}
	if r.Path != "" {
		id += "/" + strings.ToLower(r.Path)
	}
	return id
}

func firstPartyOwner(owner string) bool {
	switch strings.ToLower(owner) {
	case "actions", "github":
		return true
	}
	return false
}

func collectUses(w *model.Workflow) (map[string]*usage, []string) {
	m := make(map[string]*usage)
	var order []string
	add := func(id, ref string, kind model.RefKind, thirdParty bool, pos source.Position) {
		if id == "" {
			return
		}
		u, ok := m[id]
		if !ok {
			u = &usage{refs: make(map[string]model.RefKind), thirdParty: thirdParty}
			m[id] = u
			order = append(order, id)
		}
		if ref != "" {
			u.refs[ref] = kind
		}
		u.sites = append(u.sites, useSite{ref: ref, pos: pos})
	}
	for _, j := range w.Jobs {
		if j == nil {
			continue
		}
		if r := j.Uses; r != nil {
			add(reusableIdentity(r), r.Ref, r.Kind, !r.Local && !firstPartyOwner(r.Owner), r.Pos)
		}
		for _, s := range j.Steps {
			if s == nil || s.Uses == nil {
				continue
			}
			a := s.Uses
			add(a.Identity(), a.Ref, a.Kind, !a.Local && !a.Docker && !a.FirstParty, a.Pos)
		}
	}
	return m, order
}

func (u *usage) refList() []string {
	out := make([]string, 0, len(u.refs))
	for r := range u.refs {
		out = append(out, r)
	}
	sort.Strings(out)
	return out
}

func (u *usage) anyKind(kind model.RefKind) bool {
	for _, k := range u.refs {
		if k == kind {
			return true
		}
	}
	return false
}

func (u *usage) allSHA() bool {
	if len(u.refs) == 0 {
		return false
	}
	for _, k := range u.refs {
		if k != model.RefSHA {
			return false
		}
	}
	return true
}

func sameRefs(a, b *usage) bool {
	if len(a.refs) != len(b.refs) {
		return false
	}
	for r := range a.refs {
		if _, ok := b.refs[r]; !ok {
			return false
		}
	}
	return true
}

func (c *comparer) actionChanges() {
	bm, border := collectUses(c.before.wf)
	am, aorder := collectUses(c.after.wf)
	for _, id := range aorder {
		au := am[id]
		bu, ok := bm[id]
		if !ok {
			f := Finding{Kind: kindActionAdded, Subject: id, After: strings.Join(au.refList(), ", "), Pos: au.sites[0].pos}
			if au.thirdParty {
				f.Kind = kindActionThirdPartyAdded
				if au.anyKind(model.RefMutable) {
					f.Detail = " (mutable ref)"
				}
			}
			c.emit(f)
			continue
		}
		if sameRefs(bu, au) {
			continue
		}
		f := Finding{
			Kind:    kindActionRefChanged,
			Subject: id,
			Before:  strings.Join(bu.refList(), ", "),
			After:   strings.Join(au.refList(), ", "),
			Pos:     changedSite(bu, au),
		}
		if bu.allSHA() && au.anyKind(model.RefMutable) {
			f.Kind = kindActionPinRemoved
		}
		c.emit(f)
	}
	for _, id := range border {
		if _, ok := am[id]; ok {
			continue
		}
		bu := bm[id]
		c.emit(Finding{Kind: kindActionRemoved, Subject: id, Before: strings.Join(bu.refList(), ", "), BasePos: bu.sites[0].pos})
	}
}

func changedSite(before, after *usage) source.Position {
	for _, s := range after.sites {
		if _, ok := before.refs[s.ref]; !ok {
			return s.pos
		}
	}
	return after.sites[0].pos
}

func (c *comparer) secretChanges() {
	seen := make(map[string]bool, len(c.before.wf.SecretRefs))
	for _, s := range c.before.wf.SecretRefs {
		seen[s] = true
	}
	for _, s := range c.after.wf.SecretRefs {
		if !seen[s] {
			c.emit(Finding{Kind: kindSecretsAdded, Subject: s})
		}
	}
}
