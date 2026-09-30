package source

import (
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Kind int

const (
	KindInvalid Kind = iota
	KindScalar
	KindMapping
	KindSequence
)

type Node struct {
	doc  *Document
	raw  *yaml.Node
	val  *yaml.Node
	path string
}

type Field struct {
	Name  string
	Key   *Node
	Value *Node
}

func (n *Node) Kind() Kind {
	if n == nil || n.val == nil {
		return KindInvalid
	}
	switch n.val.Kind {
	case yaml.ScalarNode:
		return KindScalar
	case yaml.MappingNode:
		return KindMapping
	case yaml.SequenceNode:
		return KindSequence
	}
	return KindInvalid
}

func (n *Node) Exists() bool {
	return n != nil
}

func (n *Node) Pos() Position {
	if n == nil || n.doc == nil || n.raw == nil {
		return Position{}
	}
	return n.doc.sp.position(n.doc.File, n.path, n.raw)
}

func (n *Node) Path() string {
	if n == nil {
		return ""
	}
	return n.path
}

func (n *Node) Tag() string {
	if n == nil || n.val == nil {
		return ""
	}
	return n.val.ShortTag()
}

func (n *Node) IsNull() bool {
	return n.Kind() == KindScalar && n.val.ShortTag() == "!!null"
}

func (n *Node) Len() int {
	switch n.Kind() {
	case KindMapping:
		return len(n.Fields())
	case KindSequence:
		return len(n.val.Content)
	}
	return 0
}

func isMergeKey(k *yaml.Node) bool {
	return k != nil && k.Kind == yaml.ScalarNode && k.ShortTag() == "!!merge"
}

func keyName(k *yaml.Node) string {
	r := resolve(k)
	if r == nil || r.Kind != yaml.ScalarNode {
		return ""
	}
	return r.Value
}

func childPath(parent, key string) string {
	if key == "" || strings.ContainsAny(key, ".[]'\" \t") {
		return parent + "['" + strings.ReplaceAll(key, "'", "''") + "']"
	}
	if parent == "" {
		return key
	}
	return parent + "." + key
}

func indexPath(parent string, i int) string {
	return parent + "[" + strconv.Itoa(i) + "]"
}

type rawField struct {
	name string
	key  *yaml.Node
	val  *yaml.Node
}

func collectFields(m *yaml.Node, depth int) []rawField {
	if m == nil || m.Kind != yaml.MappingNode || depth > maxDepth {
		return nil
	}
	explicit := make(map[string]bool)
	for i := 0; i+1 < len(m.Content); i += 2 {
		if !isMergeKey(m.Content[i]) {
			explicit[keyName(m.Content[i])] = true
		}
	}
	seen := make(map[string]bool)
	var out []rawField
	for i := 0; i+1 < len(m.Content); i += 2 {
		k, v := m.Content[i], m.Content[i+1]
		if !isMergeKey(k) {
			name := keyName(k)
			if seen[name] {
				continue
			}
			seen[name] = true
			out = append(out, rawField{name: name, key: k, val: v})
			continue
		}
		if depth >= maxDepth {
			continue
		}
		for _, src := range mergeSources(v) {
			for _, f := range collectFields(src, depth+1) {
				if explicit[f.name] || seen[f.name] {
					continue
				}
				seen[f.name] = true
				out = append(out, f)
			}
		}
	}
	return out
}

func mergeSources(v *yaml.Node) []*yaml.Node {
	r := resolve(v)
	if r == nil {
		return nil
	}
	switch r.Kind {
	case yaml.MappingNode:
		return []*yaml.Node{r}
	case yaml.SequenceNode:
		var out []*yaml.Node
		for _, item := range r.Content {
			if ri := resolve(item); ri != nil && ri.Kind == yaml.MappingNode {
				out = append(out, ri)
			}
		}
		return out
	}
	return nil
}

func hasMerge(m *yaml.Node) bool {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if isMergeKey(m.Content[i]) {
			return true
		}
	}
	return false
}

func (n *Node) Fields() []Field {
	if n.Kind() != KindMapping {
		return nil
	}
	raw := collectFields(n.val, 0)
	out := make([]Field, 0, len(raw))
	for _, f := range raw {
		p := childPath(n.path, f.name)
		out = append(out, Field{
			Name:  f.name,
			Key:   n.doc.newNode(f.key, p),
			Value: n.doc.newNode(f.val, p),
		})
	}
	return out
}

func (n *Node) lookupField(name string) (key, val *yaml.Node) {
	if n.Kind() != KindMapping {
		return nil, nil
	}
	m := n.val
	if name == "<<" {
		for i := 0; i+1 < len(m.Content); i += 2 {
			if isMergeKey(m.Content[i]) {
				return m.Content[i], m.Content[i+1]
			}
		}
	}
	for i := 0; i+1 < len(m.Content); i += 2 {
		k := m.Content[i]
		if !isMergeKey(k) && keyName(k) == name {
			return k, m.Content[i+1]
		}
	}
	if name == "<<" || !hasMerge(m) {
		return nil, nil
	}
	for _, f := range collectFields(m, 0) {
		if f.name == name {
			return f.key, f.val
		}
	}
	return nil, nil
}

func (n *Node) Field(name string) *Node {
	_, v := n.lookupField(name)
	if v == nil {
		return nil
	}
	return n.doc.newNode(v, childPath(n.path, name))
}

func (n *Node) FieldKey(name string) *Node {
	k, _ := n.lookupField(name)
	if k == nil {
		return nil
	}
	return n.doc.newNode(k, childPath(n.path, name))
}

func (n *Node) Has(name string) bool {
	_, v := n.lookupField(name)
	return v != nil
}

func (n *Node) Keys() []string {
	fields := n.Fields()
	if fields == nil {
		return nil
	}
	out := make([]string, len(fields))
	for i, f := range fields {
		out[i] = f.Name
	}
	return out
}

func (n *Node) Items() []*Node {
	if n.Kind() != KindSequence {
		return nil
	}
	out := make([]*Node, len(n.val.Content))
	for i, c := range n.val.Content {
		out[i] = n.doc.newNode(c, indexPath(n.path, i))
	}
	return out
}

func (n *Node) Index(i int) *Node {
	if n.Kind() != KindSequence || i < 0 || i >= len(n.val.Content) {
		return nil
	}
	return n.doc.newNode(n.val.Content[i], indexPath(n.path, i))
}

func (n *Node) Str() (string, bool) {
	if n.Kind() != KindScalar || n.IsNull() {
		return "", false
	}
	return n.val.Value, true
}

func (n *Node) StrOr(fallback string) string {
	if s, ok := n.Str(); ok {
		return s
	}
	return fallback
}

func (n *Node) PosStr() (Positioned[string], bool) {
	s, ok := n.Str()
	if !ok {
		return Positioned[string]{}, false
	}
	return At(s, n.Pos()), true
}

func (n *Node) Int() (int, bool) {
	if n.Kind() != KindScalar || n.val.ShortTag() != "!!int" {
		return 0, false
	}
	return parseYAMLInt(n.val.Value)
}

func parseYAMLInt(s string) (int, bool) {
	s = strings.ReplaceAll(s, "_", "")
	neg := false
	switch {
	case strings.HasPrefix(s, "-"):
		neg = true
		s = s[1:]
	case strings.HasPrefix(s, "+"):
		s = s[1:]
	}
	base := 10
	switch {
	case strings.HasPrefix(s, "0x"), strings.HasPrefix(s, "0X"):
		base, s = 16, s[2:]
	case strings.HasPrefix(s, "0o"), strings.HasPrefix(s, "0O"):
		base, s = 8, s[2:]
	case strings.HasPrefix(s, "0b"), strings.HasPrefix(s, "0B"):
		base, s = 2, s[2:]
	}
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseInt(s, base, strconv.IntSize)
	if err != nil {
		return 0, false
	}
	if neg {
		v = -v
	}
	return int(v), true
}

func (n *Node) Bool() (bool, bool) {
	if n.Kind() != KindScalar {
		return false, false
	}
	switch strings.ToLower(n.val.Value) {
	case "true", "yes", "on", "y":
		return true, true
	case "false", "no", "off", "n":
		return false, true
	}
	return false, false
}

func (n *Node) StrList() []Positioned[string] {
	switch n.Kind() {
	case KindScalar:
		if p, ok := n.PosStr(); ok {
			return []Positioned[string]{p}
		}
	case KindSequence:
		var out []Positioned[string]
		for _, item := range n.Items() {
			if p, ok := item.PosStr(); ok {
				out = append(out, p)
			}
		}
		return out
	case KindMapping:
		var out []Positioned[string]
		for _, f := range n.Fields() {
			out = append(out, At(f.Name, f.Key.Pos()))
		}
		return out
	}
	return nil
}

func (n *Node) Walk(fn func(*Node) bool) {
	if n == nil || fn == nil {
		return
	}
	n.walk(fn)
}

func (n *Node) walk(fn func(*Node) bool) {
	if !fn(n) {
		return
	}
	switch n.raw.Kind {
	case yaml.MappingNode:
		seen := make(map[string]bool)
		for i := 0; i+1 < len(n.raw.Content); i += 2 {
			k := n.raw.Content[i]
			name := "<<"
			if !isMergeKey(k) {
				name = keyName(k)
			}
			if seen[name] {
				continue
			}
			seen[name] = true
			n.doc.newNode(n.raw.Content[i+1], childPath(n.path, name)).walk(fn)
		}
	case yaml.SequenceNode:
		for i, c := range n.raw.Content {
			n.doc.newNode(c, indexPath(n.path, i)).walk(fn)
		}
	}
}

func (n *Node) Lookup(path string) *Node {
	cur := n
	i := 0
	for i < len(path) {
		if cur == nil {
			return nil
		}
		switch {
		case path[i] == '[' && i+1 < len(path) && path[i+1] == '\'':
			j := i + 2
			var b strings.Builder
			closed := false
			for j < len(path) {
				if path[j] == '\'' {
					if j+1 < len(path) && path[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					closed = true
					break
				}
				b.WriteByte(path[j])
				j++
			}
			if !closed || j+1 >= len(path) || path[j+1] != ']' {
				return nil
			}
			cur = cur.Field(b.String())
			i = j + 2
		case path[i] == '[':
			j := strings.IndexByte(path[i:], ']')
			if j < 0 {
				return nil
			}
			idx, err := strconv.Atoi(path[i+1 : i+j])
			if err != nil {
				return nil
			}
			cur = cur.Index(idx)
			i += j + 1
		default:
			start := i
			if path[i] == '.' {
				if i == 0 {
					return nil
				}
				start = i + 1
			} else if i != 0 {
				return nil
			}
			j := start
			for j < len(path) && path[j] != '.' && path[j] != '[' {
				j++
			}
			if j == start {
				return nil
			}
			cur = cur.Field(path[start:j])
			i = j
		}
	}
	return cur
}
