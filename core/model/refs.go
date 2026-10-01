package model

import (
	"regexp"
	"strings"

	"github.com/BETAER-08/quanto/core/source"
)

var shaRef = regexp.MustCompile(`^[0-9a-fA-F]{40}$`)

func refKind(ref string) RefKind {
	if shaRef.MatchString(ref) {
		return RefSHA
	}
	return RefMutable
}

func splitRemote(name string) (owner, repo, path string) {
	parts := strings.SplitN(name, "/", 3)
	owner = parts[0]
	if len(parts) > 1 {
		repo = parts[1]
	}
	if len(parts) > 2 {
		path = parts[2]
	}
	return owner, repo, path
}

func parseActionRef(raw string, pos source.Position) (*ActionRef, bool) {
	a := &ActionRef{Raw: raw, Pos: pos}
	switch {
	case strings.HasPrefix(raw, "./"), strings.HasPrefix(raw, `.\`):
		a.Local = true
		a.Path = raw
		return a, true
	case strings.HasPrefix(raw, "docker://"):
		a.Docker = true
		a.DockerImage = strings.TrimPrefix(raw, "docker://")
		return a, true
	}
	name, ref, hasRef := strings.Cut(raw, "@")
	a.Owner, a.Repo, a.Path = splitRemote(name)
	switch strings.ToLower(a.Owner) {
	case "actions", "github":
		a.FirstParty = true
	}
	if !hasRef {
		a.Kind = RefUnknown
		return a, false
	}
	a.Ref = ref
	a.Kind = refKind(ref)
	return a, true
}

func parseReusableRef(raw string, pos source.Position) *ReusableRef {
	r := &ReusableRef{Raw: raw, Pos: pos}
	if strings.HasPrefix(raw, "./") {
		r.Local = true
		r.Path = raw
		return r
	}
	name, ref, hasRef := strings.Cut(raw, "@")
	r.Owner, r.Repo, r.Path = splitRemote(name)
	if hasRef {
		r.Ref = ref
		r.Kind = refKind(ref)
	}
	return r
}
