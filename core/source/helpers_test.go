package source

import (
	"strings"
	"testing"
)

func normalizedLines(content string) []string {
	text := strings.ReplaceAll(content, "\r\n", "\n")
	text = strings.ReplaceAll(text, "\r", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	lines := strings.Split(text, "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}

func textAt(content string, pos Position) string {
	lines := normalizedLines(content)
	if !pos.Valid() || pos.EndLine > len(lines) {
		return ""
	}
	var b strings.Builder
	for l := pos.Line; l <= pos.EndLine; l++ {
		r := []rune(lines[l-1])
		from := 1
		to := len(r)
		if l == pos.Line {
			from = pos.Column
		}
		if l == pos.EndLine {
			to = pos.EndColumn
		}
		if to > len(r) {
			to = len(r)
		}
		if from <= to {
			b.WriteString(string(r[from-1 : to]))
		}
		if l != pos.EndLine {
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func trimmedWidth(line string) int {
	return len([]rune(strings.TrimRight(line, " \t")))
}

func mustLoad(t *testing.T, content string) *Document {
	t.Helper()
	d, err := Load("test.yml", []byte(content))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return d
}

func mustLookup(t *testing.T, d *Document, path string) *Node {
	t.Helper()
	n := d.Root().Lookup(path)
	if n == nil {
		t.Fatalf("Lookup(%q) = nil", path)
	}
	return n
}

func splitPath(path string) []string {
	var segs []string
	i := 0
	for i < len(path) {
		switch {
		case path[i] == '[' && i+1 < len(path) && path[i+1] == '\'':
			j := i + 2
			for j < len(path) {
				if path[j] == '\'' {
					if j+1 < len(path) && path[j+1] == '\'' {
						j += 2
						continue
					}
					break
				}
				j++
			}
			segs = append(segs, path[i:j+2])
			i = j + 2
		case path[i] == '[':
			j := strings.IndexByte(path[i:], ']')
			segs = append(segs, path[i:i+j+1])
			i += j + 1
		default:
			start := i
			if path[i] == '.' {
				i++
			}
			for i < len(path) && path[i] != '.' && path[i] != '[' {
				i++
			}
			segs = append(segs, path[start:i])
		}
	}
	return segs
}

func parentPath(path string) (string, bool) {
	segs := splitPath(path)
	if len(segs) == 0 {
		return "", false
	}
	return strings.Join(segs[:len(segs)-1], ""), true
}

func inside(child, parent Position) bool {
	startOK := child.Line > parent.Line || (child.Line == parent.Line && child.Column >= parent.Column)
	endOK := child.EndLine < parent.EndLine || (child.EndLine == parent.EndLine && child.EndColumn <= parent.EndColumn)
	return startOK && endOK
}

func checkBounds(t *testing.T, name string, d *Document, content string, pos Position) {
	t.Helper()
	lines := normalizedLines(content)
	if !pos.Valid() {
		t.Errorf("%s: invalid position %v", name, pos)
		return
	}
	if pos.Line < 1 || pos.EndLine > d.LineCount() {
		t.Errorf("%s: line range %v outside 1..%d", name, pos, d.LineCount())
		return
	}
	maxStart := trimmedWidth(lines[pos.Line-1])
	if maxStart < 1 {
		maxStart = 1
	}
	maxEnd := trimmedWidth(lines[pos.EndLine-1])
	if maxEnd < 1 {
		maxEnd = 1
	}
	if pos.Column > maxStart || pos.EndColumn > maxEnd {
		t.Errorf("%s: column range %v exceeds line widths %d/%d", name, pos, maxStart, maxEnd)
	}
}

func checkInvariants(t *testing.T, name, content string) int {
	t.Helper()
	d, err := Load(name, []byte(content))
	if err != nil {
		t.Errorf("%s: Load: %v", name, err)
		return 0
	}
	if d.Empty() {
		return 0
	}
	root := d.Root()
	positions := make(map[string]Position)
	count := 0
	root.Walk(func(n *Node) bool {
		count++
		pos := n.Pos()
		label := name + " " + n.Path()
		checkBounds(t, label, d, content, pos)
		if pos.Valid() && textAt(content, pos) == "" && trimmedWidth(normalizedLines(content)[pos.Line-1]) > 0 {
			t.Errorf("%s: empty text at %v", label, pos)
		}
		if pos.Path != n.Path() {
			t.Errorf("%s: Position.Path %q != Path %q", label, pos.Path, n.Path())
		}
		back := root.Lookup(n.Path())
		if back == nil {
			t.Errorf("%s: Lookup returned nil", label)
		} else if back.Pos() != pos {
			t.Errorf("%s: Lookup position %v != %v", label, back.Pos(), pos)
		}
		if _, dup := positions[n.Path()]; !dup {
			positions[n.Path()] = pos
		}
		if parent, ok := parentPath(n.Path()); ok {
			if pp, found := positions[parent]; found && !inside(pos, pp) {
				t.Errorf("%s: span %v not inside parent %q %v", label, pos, parent, pp)
			}
		}
		for _, f := range n.Fields() {
			checkBounds(t, label+" key "+f.Name, d, content, f.Key.Pos())
		}
		return true
	})
	return count
}
