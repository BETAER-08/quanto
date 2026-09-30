package source

import "fmt"

type Position struct {
	File      string
	Line      int
	Column    int
	EndLine   int
	EndColumn int
	Path      string
}

func (p Position) Valid() bool {
	if p.Line < 1 || p.Column < 1 || p.EndLine < p.Line || p.EndColumn < 1 {
		return false
	}
	if p.EndLine == p.Line && p.EndColumn < p.Column {
		return false
	}
	return true
}

func (p Position) Contains(line, column int) bool {
	if !p.Valid() {
		return false
	}
	if line < p.Line || line > p.EndLine {
		return false
	}
	if line == p.Line && column < p.Column {
		return false
	}
	if line == p.EndLine && column > p.EndColumn {
		return false
	}
	return true
}

func (p Position) Cover(q Position) Position {
	if !p.Valid() {
		return q
	}
	if !q.Valid() {
		return p
	}
	out := p
	if q.Line < out.Line || (q.Line == out.Line && q.Column < out.Column) {
		out.Line = q.Line
		out.Column = q.Column
	}
	if q.EndLine > out.EndLine || (q.EndLine == out.EndLine && q.EndColumn > out.EndColumn) {
		out.EndLine = q.EndLine
		out.EndColumn = q.EndColumn
	}
	return out
}

func (p Position) String() string {
	if p.File == "" {
		return fmt.Sprintf("%d:%d-%d:%d", p.Line, p.Column, p.EndLine, p.EndColumn)
	}
	return fmt.Sprintf("%s:%d:%d-%d:%d", p.File, p.Line, p.Column, p.EndLine, p.EndColumn)
}

type Positioned[T any] struct {
	Value T
	Pos   Position
}

func At[T any](v T, pos Position) Positioned[T] {
	return Positioned[T]{Value: v, Pos: pos}
}
