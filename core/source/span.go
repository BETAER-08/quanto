package source

import (
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type point struct {
	line int
	col  int
}

func (a point) less(b point) bool {
	if a.line != b.line {
		return a.line < b.line
	}
	return a.col < b.col
}

type span struct {
	start point
	end   point
}

type spanner struct {
	lines  [][]rune
	widths []int
	spans  map[*yaml.Node]span
}

func newSpanner(content []byte) *spanner {
	text := string(content)
	text = strings.ReplaceAll(text, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	raw := strings.Split(text, "\n")
	if len(raw) > 0 && raw[len(raw)-1] == "" {
		raw = raw[:len(raw)-1]
	}
	s := &spanner{
		lines:  make([][]rune, len(raw)),
		widths: make([]int, len(raw)),
		spans:  make(map[*yaml.Node]span),
	}
	for i, l := range raw {
		r := []rune(l)
		s.lines[i] = r
		w := len(r)
		for w > 0 && (r[w-1] == ' ' || r[w-1] == '\t') {
			w--
		}
		s.widths[i] = w
	}
	return s
}

func (s *spanner) lineCount() int {
	return len(s.lines)
}

func (s *spanner) width(line int) int {
	if line < 1 || line > len(s.lines) {
		return 0
	}
	return s.widths[line-1]
}

func (s *spanner) at(p point) (rune, bool) {
	if p.line < 1 || p.line > len(s.lines) {
		return 0, false
	}
	l := s.lines[p.line-1]
	if p.col < 1 || p.col > len(l) {
		return 0, false
	}
	return l[p.col-1], true
}

func (s *spanner) next(p point) (point, bool) {
	if p.line < 1 {
		return point{1, 1}, len(s.lines) > 0
	}
	if p.line > len(s.lines) {
		return p, false
	}
	if p.col < len(s.lines[p.line-1]) {
		return point{p.line, p.col + 1}, true
	}
	if p.line >= len(s.lines) {
		return p, false
	}
	return point{p.line + 1, 1}, true
}

func (s *spanner) clamp(p point) point {
	n := len(s.lines)
	if n == 0 {
		return point{1, 1}
	}
	if p.line < 1 {
		p.line = 1
		p.col = 1
	}
	if p.line > n {
		p.line = n
		p.col = s.widths[n-1]
	}
	max := s.widths[p.line-1]
	if max < 1 {
		max = 1
	}
	if p.col < 1 {
		p.col = 1
	}
	if p.col > max {
		p.col = max
	}
	return p
}

func (s *spanner) indent(line int) int {
	if line < 1 || line > len(s.lines) {
		return 0
	}
	n := 0
	for _, r := range s.lines[line-1] {
		if r != ' ' {
			break
		}
		n++
	}
	return n
}

func (s *spanner) position(file, path string, n *yaml.Node) Position {
	sp, ok := s.spans[n]
	if !ok {
		p := s.clamp(point{n.Line, n.Column})
		sp = span{start: p, end: p}
	}
	return Position{
		File:      file,
		Line:      sp.start.line,
		Column:    sp.start.col,
		EndLine:   sp.end.line,
		EndColumn: sp.end.col,
		Path:      path,
	}
}

func (s *spanner) compute(n *yaml.Node, parentIndent int) span {
	if sp, ok := s.spans[n]; ok {
		return sp
	}
	start := s.clamp(point{n.Line, n.Column})
	end := start
	switch n.Kind {
	case yaml.MappingNode, yaml.SequenceNode:
		maxEnd := start
		minStart := start
		for i, c := range n.Content {
			childIndent := parentIndent
			if n.Kind == yaml.MappingNode && i%2 == 1 {
				childIndent = n.Content[i-1].Column - 1
			}
			if n.Kind == yaml.SequenceNode {
				childIndent = n.Column - 1
			}
			cs := s.compute(c, childIndent)
			if maxEnd.less(cs.end) {
				maxEnd = cs.end
			}
			if cs.start.less(minStart) {
				minStart = cs.start
			}
		}
		start = minStart
		end = maxEnd
		if n.Style&yaml.FlowStyle != 0 {
			end = s.closeFlow(start, maxEnd, len(n.Content) > 0)
		}
	case yaml.ScalarNode:
		end = s.scalarEnd(n, start, parentIndent)
	case yaml.AliasNode:
		end = point{start.line, start.col + utf8.RuneCountInString(n.Value)}
	case yaml.DocumentNode:
		for _, c := range n.Content {
			cs := s.compute(c, -1)
			if end.less(cs.end) {
				end = cs.end
			}
		}
	}
	end = s.clamp(end)
	if end.less(start) {
		end = start
	}
	sp := span{start: start, end: end}
	s.spans[n] = sp
	return sp
}

func (s *spanner) skipProps(p point) point {
	for i := 0; i < 8; i++ {
		c, ok := s.at(p)
		if !ok {
			return p
		}
		if c != '&' && c != '!' {
			return p
		}
		for {
			c, ok = s.at(p)
			if !ok || c == ' ' || c == '\t' {
				break
			}
			p.col++
		}
		for {
			c, ok = s.at(p)
			if !ok || (c != ' ' && c != '\t') {
				break
			}
			p.col++
		}
		if _, ok = s.at(p); !ok {
			found := false
			for l := p.line + 1; l <= len(s.lines); l++ {
				if s.width(l) == 0 {
					continue
				}
				p = point{l, s.indent(l) + 1}
				found = true
				break
			}
			if !found {
				return p
			}
		}
	}
	return p
}

func (s *spanner) scalarEnd(n *yaml.Node, start point, parentIndent int) point {
	p := s.skipProps(start)
	c, _ := s.at(p)
	switch {
	case n.Style&yaml.DoubleQuotedStyle != 0 && c == '"':
		return s.scanQuoted(p, '"')
	case n.Style&yaml.SingleQuotedStyle != 0 && c == '\'':
		return s.scanQuoted(p, '\'')
	case n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 && (c == '|' || c == '>'):
		return s.blockEnd(p, parentIndent)
	}
	return s.plainEnd(n, p, parentIndent)
}

func (s *spanner) scanQuoted(p point, q rune) point {
	last := p
	cur := p
	ok := true
	for {
		cur, ok = s.next(cur)
		if !ok {
			return last
		}
		c, has := s.at(cur)
		if !has {
			continue
		}
		last = cur
		if q == '"' && c == '\\' {
			nx, nok := s.next(cur)
			if !nok {
				return last
			}
			if _, h := s.at(nx); h && nx.line == cur.line {
				cur = nx
				last = cur
			}
			continue
		}
		if c == q {
			if q == '\'' {
				nx := point{cur.line, cur.col + 1}
				if nc, h := s.at(nx); h && nc == '\'' {
					cur = nx
					last = cur
					continue
				}
			}
			return cur
		}
	}
}

func (s *spanner) blockEnd(p point, parentIndent int) point {
	end := p
	for {
		c, ok := s.at(point{end.line, end.col + 1})
		if !ok || c == ' ' || c == '\t' {
			break
		}
		end.col++
	}
	contentIndent := -1
	for l := p.line + 1; l <= len(s.lines); l++ {
		if s.width(l) == 0 {
			continue
		}
		ind := s.indent(l)
		if contentIndent < 0 {
			if ind <= parentIndent {
				break
			}
			contentIndent = ind
		} else if ind < contentIndent {
			break
		}
		end = point{l, s.width(l)}
	}
	return end
}

func (s *spanner) plainEnd(n *yaml.Node, p point, parentIndent int) point {
	vl := utf8.RuneCountInString(n.Value)
	if vl == 0 {
		return p
	}
	end := point{p.line, p.col + vl - 1}
	if end.col <= s.width(p.line) {
		return end
	}
	last := point{p.line, s.width(p.line)}
	contIndent := -1
	for l := p.line + 1; l <= len(s.lines); l++ {
		w := s.width(l)
		if w == 0 {
			continue
		}
		ind := s.indent(l)
		if ind <= parentIndent {
			break
		}
		trimmed := string(s.lines[l-1][ind:w])
		if strings.HasPrefix(trimmed, "#") || trimmed == "-" || strings.HasPrefix(trimmed, "- ") {
			break
		}
		if strings.HasSuffix(trimmed, ":") || strings.Contains(trimmed, ": ") {
			break
		}
		if ind == 0 && (strings.HasPrefix(trimmed, "---") || strings.HasPrefix(trimmed, "...")) {
			break
		}
		if contIndent < 0 {
			contIndent = ind
		} else if ind < contIndent {
			break
		}
		e := w
		if i := strings.Index(trimmed, " #"); i >= 0 {
			e = ind + utf8.RuneCountInString(strings.TrimRight(trimmed[:i], " \t"))
		}
		last = point{l, e}
	}
	return last
}

func (s *spanner) closeFlow(start, maxEnd point, hasChildren bool) point {
	p := s.skipProps(start)
	open, ok := s.at(p)
	if !ok {
		return maxEnd
	}
	var closer rune
	switch open {
	case '[':
		closer = ']'
	case '{':
		closer = '}'
	default:
		return maxEnd
	}
	cur := p
	if hasChildren && p.less(maxEnd) {
		cur = maxEnd
	}
	for {
		nx, more := s.next(cur)
		if !more {
			return maxEnd
		}
		cur = nx
		c, has := s.at(cur)
		if !has {
			continue
		}
		switch {
		case c == closer:
			return cur
		case c == ' ' || c == '\t' || c == ',':
			continue
		case c == '#':
			cur = point{cur.line, len(s.lines[cur.line-1])}
		default:
			return maxEnd
		}
	}
}
