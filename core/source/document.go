package source

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type Document struct {
	File    string
	Content []byte
	root    *yaml.Node
	sp      *spanner
}

type SyntaxError struct {
	File    string
	Line    int
	Message string
	Cause   error
}

func (e *SyntaxError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Message)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Message)
}

func (e *SyntaxError) Unwrap() error {
	return e.Cause
}

func (e *SyntaxError) Pos() Position {
	if e.Line < 1 {
		return Position{File: e.File}
	}
	return Position{File: e.File, Line: e.Line, Column: 1, EndLine: e.Line, EndColumn: 1}
}

var yamlLineError = regexp.MustCompile(`^yaml: line (\d+): (.*)$`)

func newSyntaxError(file string, err error, sp *spanner) *SyntaxError {
	msg := err.Error()
	se := &SyntaxError{File: file, Message: strings.TrimPrefix(msg, "yaml: "), Cause: err}
	var te *yaml.TypeError
	if errors.As(err, &te) && len(te.Errors) > 0 {
		msg = te.Errors[0]
		se.Message = msg
	}
	if m := yamlLineError.FindStringSubmatch(msg); m != nil {
		se.Message = m[2]
		if n, convErr := strconv.Atoi(m[1]); convErr == nil {
			if n < 1 {
				n = 1
			}
			if c := sp.lineCount(); c > 0 && n > c {
				n = c
			}
			se.Line = n
		}
	}
	return se
}

func Load(file string, content []byte) (*Document, error) {
	d := &Document{File: file, Content: content, sp: newSpanner(content)}
	var doc yaml.Node
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return nil, newSyntaxError(file, err, d.sp)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) == 0 || doc.Content[0] == nil {
		return d, nil
	}
	d.root = doc.Content[0]
	d.sp.compute(d.root, -1)
	return d, nil
}

func (d *Document) Root() *Node {
	if d == nil || d.root == nil {
		return nil
	}
	return d.newNode(d.root, "")
}

func (d *Document) Empty() bool {
	return d == nil || d.root == nil
}

func (d *Document) LineCount() int {
	if d == nil || d.sp == nil {
		return 0
	}
	return d.sp.lineCount()
}

func (d *Document) newNode(raw *yaml.Node, path string) *Node {
	if raw == nil {
		return nil
	}
	return &Node{doc: d, raw: raw, val: resolve(raw), path: path}
}

func resolve(n *yaml.Node) *yaml.Node {
	for i := 0; i < maxDepth && n != nil && n.Kind == yaml.AliasNode; i++ {
		n = n.Alias
	}
	if n != nil && n.Kind == yaml.AliasNode {
		return nil
	}
	return n
}

const maxDepth = 64
