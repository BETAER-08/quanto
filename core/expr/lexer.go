package expr

import (
	"fmt"
	"strconv"
	"strings"
)

type tokenKind int

const (
	tokEOF tokenKind = iota
	tokNumber
	tokString
	tokIdent
	tokLParen
	tokRParen
	tokLBracket
	tokRBracket
	tokDot
	tokComma
	tokStar
	tokNot
	tokLT
	tokLE
	tokGT
	tokGE
	tokEQ
	tokNE
	tokAnd
	tokOr
	tokClose
)

type token struct {
	kind  tokenKind
	text  string
	value any
	off   int
}

type SyntaxError struct {
	Offset  int
	Message string
}

func (e *SyntaxError) Error() string {
	return fmt.Sprintf("offset %d: %s", e.Offset, e.Message)
}

func syntaxErrorf(off int, format string, args ...any) *SyntaxError {
	return &SyntaxError{Offset: off, Message: fmt.Sprintf(format, args...)}
}

type lexer struct {
	src []rune
	pos int
	end int
}

func isLetter(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isDigit(r rune) bool {
	return r >= '0' && r <= '9'
}

func isHexDigit(r rune) bool {
	return isDigit(r) || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')
}

func isSpace(r rune) bool {
	return r == ' ' || r == '\t' || r == '\n' || r == '\r'
}

func (l *lexer) peekAt(i int) rune {
	if i >= l.end {
		return 0
	}
	return l.src[i]
}

func (l *lexer) next() (token, error) {
	for l.pos < l.end && isSpace(l.src[l.pos]) {
		l.pos++
	}
	if l.pos >= l.end {
		return token{kind: tokEOF, off: l.pos}, nil
	}
	start := l.pos
	r := l.src[l.pos]
	single := func(k tokenKind) (token, error) {
		l.pos++
		return token{kind: k, text: string(r), off: start}, nil
	}
	double := func(k tokenKind) (token, error) {
		l.pos += 2
		return token{kind: k, text: string(l.src[start:l.pos]), off: start}, nil
	}
	n := l.peekAt(l.pos + 1)
	switch {
	case r == '(':
		return single(tokLParen)
	case r == ')':
		return single(tokRParen)
	case r == '[':
		return single(tokLBracket)
	case r == ']':
		return single(tokRBracket)
	case r == ',':
		return single(tokComma)
	case r == '*':
		return single(tokStar)
	case r == '.' && !isDigit(n):
		return single(tokDot)
	case r == '!' && n == '=':
		return double(tokNE)
	case r == '!':
		return single(tokNot)
	case r == '<' && n == '=':
		return double(tokLE)
	case r == '<':
		return single(tokLT)
	case r == '>' && n == '=':
		return double(tokGE)
	case r == '>':
		return single(tokGT)
	case r == '=' && n == '=':
		return double(tokEQ)
	case r == '&' && n == '&':
		return double(tokAnd)
	case r == '|' && n == '|':
		return double(tokOr)
	case r == '}' && n == '}':
		return double(tokClose)
	case r == '\'':
		return l.lexString()
	case isDigit(r) || r == '-' || r == '+' || r == '.':
		return l.lexNumber()
	case isLetter(r):
		l.pos++
		for l.pos < l.end {
			c := l.src[l.pos]
			if !isLetter(c) && !isDigit(c) && c != '_' && c != '-' {
				break
			}
			l.pos++
		}
		return token{kind: tokIdent, text: string(l.src[start:l.pos]), off: start}, nil
	}
	return token{}, syntaxErrorf(start, "unexpected character %q", r)
}

func (l *lexer) lexString() (token, error) {
	start := l.pos
	l.pos++
	var b strings.Builder
	for l.pos < l.end {
		c := l.src[l.pos]
		if c == '\'' {
			if l.peekAt(l.pos+1) == '\'' {
				b.WriteRune('\'')
				l.pos += 2
				continue
			}
			l.pos++
			return token{kind: tokString, text: string(l.src[start:l.pos]), value: b.String(), off: start}, nil
		}
		b.WriteRune(c)
		l.pos++
	}
	return token{}, syntaxErrorf(start, "unterminated string literal")
}

func (l *lexer) lexNumber() (token, error) {
	start := l.pos
	i := l.pos
	if c := l.peekAt(i); c == '-' || c == '+' {
		i++
	}
	if l.peekAt(i) == '0' && (l.peekAt(i+1) == 'x' || l.peekAt(i+1) == 'X') {
		j := i + 2
		for j < l.end && isHexDigit(l.src[j]) {
			j++
		}
		if j == i+2 {
			return token{}, syntaxErrorf(start, "invalid hexadecimal number")
		}
		if err := l.checkNumberEnd(j, start); err != nil {
			return token{}, err
		}
		v, err := strconv.ParseUint(string(l.src[i+2:j]), 16, 64)
		if err != nil {
			return token{}, syntaxErrorf(start, "invalid hexadecimal number")
		}
		f := float64(v)
		if l.src[start] == '-' {
			f = -f
		}
		l.pos = j
		return token{kind: tokNumber, text: string(l.src[start:j]), value: f, off: start}, nil
	}
	j := i
	if l.peekAt(j) == '0' {
		j++
	} else if isDigit(l.peekAt(j)) {
		for j < l.end && isDigit(l.src[j]) {
			j++
		}
	} else {
		return token{}, syntaxErrorf(start, "invalid number")
	}
	if l.peekAt(j) == '.' {
		k := j + 1
		for k < l.end && isDigit(l.src[k]) {
			k++
		}
		if k == j+1 {
			return token{}, syntaxErrorf(start, "invalid number")
		}
		j = k
	}
	if c := l.peekAt(j); c == 'e' || c == 'E' {
		k := j + 1
		if c2 := l.peekAt(k); c2 == '+' || c2 == '-' {
			k++
		}
		d := k
		for k < l.end && isDigit(l.src[k]) {
			k++
		}
		if k == d {
			return token{}, syntaxErrorf(start, "invalid number")
		}
		j = k
	}
	if err := l.checkNumberEnd(j, start); err != nil {
		return token{}, err
	}
	text := string(l.src[start:j])
	f, err := strconv.ParseFloat(strings.TrimPrefix(text, "+"), 64)
	if err != nil {
		return token{}, syntaxErrorf(start, "invalid number")
	}
	l.pos = j
	return token{kind: tokNumber, text: text, value: f, off: start}, nil
}

func (l *lexer) checkNumberEnd(j, start int) error {
	if c := l.peekAt(j); isLetter(c) || isDigit(c) || c == '_' || c == '.' {
		return syntaxErrorf(start, "invalid number")
	}
	return nil
}
