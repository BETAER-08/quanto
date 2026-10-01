package expr

import (
	"sort"
	"strings"
)

func indexRunes(src []rune, from int, pat string) int {
	p := []rune(pat)
	for i := from; i+len(p) <= len(src); i++ {
		match := true
		for j := range p {
			if src[i+j] != p[j] {
				match = false
				break
			}
		}
		if match {
			return i
		}
	}
	return -1
}

func ParseTemplate(s string) (*Template, error) {
	src := []rune(s)
	t := &Template{}
	pos := 0
	for pos < len(src) {
		open := indexRunes(src, pos, "${{")
		if open < 0 {
			t.Segments = append(t.Segments, Segment{Text: string(src[pos:]), Offset: pos})
			break
		}
		if open > pos {
			t.Segments = append(t.Segments, Segment{Text: string(src[pos:open]), Offset: pos})
		}
		e, end, err := parseRange(src, open+3, len(src), tokClose)
		if err != nil {
			if se, ok := err.(*SyntaxError); ok && se.Offset >= len(src) {
				return nil, syntaxErrorf(open, "unclosed expression")
			}
			return nil, err
		}
		t.Segments = append(t.Segments, Segment{Text: string(src[open:end]), Expr: e, IsExpr: true, Offset: open})
		pos = end
	}
	return t, nil
}

func ParseCondition(s string) (*Template, error) {
	if !IsDynamic(s) {
		e, err := ParseExpression(s)
		if err != nil {
			return nil, err
		}
		return &Template{Segments: []Segment{{Text: s, Expr: e, IsExpr: true, Offset: 0}}}, nil
	}
	t, err := ParseTemplate(s)
	if err != nil {
		return nil, err
	}
	var exprs []Segment
	for _, seg := range t.Segments {
		if seg.IsExpr {
			exprs = append(exprs, seg)
			continue
		}
		if strings.TrimSpace(seg.Text) != "" {
			return t, nil
		}
	}
	if len(exprs) == 1 {
		return &Template{Segments: exprs}, nil
	}
	return t, nil
}

func IsDynamic(s string) bool {
	return strings.Contains(s, "${{")
}

func References(e Expr) []Reference {
	var out []Reference
	collectRefs(e, &out)
	return normalizeRefs(out)
}

func TemplateReferences(t *Template) []Reference {
	if t == nil {
		return nil
	}
	var out []Reference
	for _, seg := range t.Segments {
		if seg.IsExpr {
			collectRefs(seg.Expr, &out)
		}
	}
	return normalizeRefs(out)
}

func collectRefs(e Expr, out *[]Reference) {
	switch x := e.(type) {
	case *Ident, *Property, *Index, *Star:
		var rev []string
		cur := e
		for {
			switch c := cur.(type) {
			case *Property:
				rev = append(rev, strings.ToLower(c.Name))
				cur = c.Target
				continue
			case *Star:
				rev = append(rev, "*")
				cur = c.Target
				continue
			case *Index:
				if lit, ok := c.Index.(*Literal); ok {
					if str, ok := lit.Value.(string); ok {
						rev = append(rev, strings.ToLower(str))
						cur = c.Target
						continue
					}
				}
				rev = append(rev, "*")
				collectRefs(c.Index, out)
				cur = c.Target
				continue
			case *Ident:
				var path []string
				for i := len(rev) - 1; i >= 0; i-- {
					path = append(path, rev[i])
				}
				*out = append(*out, Reference{Context: strings.ToLower(c.Name), Path: path})
			default:
				collectRefs(cur, out)
			}
			break
		}
	case *Unary:
		collectRefs(x.X, out)
	case *Binary:
		collectRefs(x.Left, out)
		collectRefs(x.Right, out)
	case *Call:
		for _, a := range x.Args {
			collectRefs(a, out)
		}
	}
}

func compareRefs(a, b Reference) int {
	if a.Context != b.Context {
		if a.Context < b.Context {
			return -1
		}
		return 1
	}
	for i := 0; i < len(a.Path) && i < len(b.Path); i++ {
		if a.Path[i] != b.Path[i] {
			if a.Path[i] < b.Path[i] {
				return -1
			}
			return 1
		}
	}
	return len(a.Path) - len(b.Path)
}

func normalizeRefs(refs []Reference) []Reference {
	if len(refs) == 0 {
		return nil
	}
	sort.SliceStable(refs, func(i, j int) bool { return compareRefs(refs[i], refs[j]) < 0 })
	out := refs[:1]
	for _, r := range refs[1:] {
		if compareRefs(out[len(out)-1], r) != 0 {
			out = append(out, r)
		}
	}
	return out
}

func Functions(e Expr) []string {
	seen := make(map[string]bool)
	collectFuncs(e, seen)
	if len(seen) == 0 {
		return nil
	}
	out := make([]string, 0, len(seen))
	for name := range seen {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func collectFuncs(e Expr, seen map[string]bool) {
	switch x := e.(type) {
	case *Property:
		collectFuncs(x.Target, seen)
	case *Star:
		collectFuncs(x.Target, seen)
	case *Index:
		collectFuncs(x.Target, seen)
		collectFuncs(x.Index, seen)
	case *Unary:
		collectFuncs(x.X, seen)
	case *Binary:
		collectFuncs(x.Left, seen)
		collectFuncs(x.Right, seen)
	case *Call:
		seen[x.Name] = true
		for _, a := range x.Args {
			collectFuncs(a, seen)
		}
	}
}
