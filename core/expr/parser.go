package expr

import (
	"strings"
)

const maxNesting = 256

type parser struct {
	lex   *lexer
	tok   token
	depth int
}

func newParser(src []rune, start, end int) (*parser, error) {
	p := &parser{lex: &lexer{src: src, pos: start, end: end}}
	if err := p.advance(); err != nil {
		return nil, err
	}
	return p, nil
}

func (p *parser) advance() error {
	t, err := p.lex.next()
	if err != nil {
		return err
	}
	p.tok = t
	return nil
}

func (p *parser) expect(k tokenKind, what string) error {
	if p.tok.kind != k {
		return p.unexpected(what)
	}
	return p.advance()
}

func (p *parser) unexpected(what string) error {
	if p.tok.kind == tokEOF {
		return syntaxErrorf(p.tok.off, "unexpected end of expression, expected %s", what)
	}
	return syntaxErrorf(p.tok.off, "unexpected %q, expected %s", p.tok.text, what)
}

func (p *parser) enter() error {
	p.depth++
	if p.depth > maxNesting {
		return syntaxErrorf(p.tok.off, "expression nested too deeply")
	}
	return nil
}

func (p *parser) leave() {
	p.depth--
}

func (p *parser) parseExpr() (Expr, error) {
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	return p.parseOr()
}

func (p *parser) parseBinaryLevel(next func() (Expr, error), ops map[tokenKind]bool) (Expr, error) {
	left, err := next()
	if err != nil {
		return nil, err
	}
	for ops[p.tok.kind] {
		op := p.tok.text
		if err := p.advance(); err != nil {
			return nil, err
		}
		right, err := next()
		if err != nil {
			return nil, err
		}
		left = &Binary{Op: op, Left: left, Right: right, Off: left.Offset()}
	}
	return left, nil
}

var (
	orOps  = map[tokenKind]bool{tokOr: true}
	andOps = map[tokenKind]bool{tokAnd: true}
	eqOps  = map[tokenKind]bool{tokEQ: true, tokNE: true}
	cmpOps = map[tokenKind]bool{tokLT: true, tokLE: true, tokGT: true, tokGE: true}
)

func (p *parser) parseOr() (Expr, error) {
	return p.parseBinaryLevel(p.parseAnd, orOps)
}

func (p *parser) parseAnd() (Expr, error) {
	return p.parseBinaryLevel(p.parseEquality, andOps)
}

func (p *parser) parseEquality() (Expr, error) {
	return p.parseBinaryLevel(p.parseComparison, eqOps)
}

func (p *parser) parseComparison() (Expr, error) {
	return p.parseBinaryLevel(p.parseUnary, cmpOps)
}

func (p *parser) parseUnary() (Expr, error) {
	if p.tok.kind != tokNot {
		return p.parsePostfix()
	}
	if err := p.enter(); err != nil {
		return nil, err
	}
	defer p.leave()
	off := p.tok.off
	if err := p.advance(); err != nil {
		return nil, err
	}
	x, err := p.parseUnary()
	if err != nil {
		return nil, err
	}
	return &Unary{Op: "!", X: x, Off: off}, nil
}

func (p *parser) parsePostfix() (Expr, error) {
	e, err := p.parsePrimary()
	if err != nil {
		return nil, err
	}
	for {
		switch p.tok.kind {
		case tokDot:
			if err := p.advance(); err != nil {
				return nil, err
			}
			switch p.tok.kind {
			case tokStar:
				e = &Star{Target: e, Off: e.Offset()}
			case tokIdent:
				e = &Property{Target: e, Name: p.tok.text, Off: e.Offset()}
			default:
				return nil, p.unexpected("property name or '*'")
			}
			if err := p.advance(); err != nil {
				return nil, err
			}
		case tokLBracket:
			if err := p.advance(); err != nil {
				return nil, err
			}
			idx, err := p.parseExpr()
			if err != nil {
				return nil, err
			}
			if err := p.expect(tokRBracket, "']'"); err != nil {
				return nil, err
			}
			e = &Index{Target: e, Index: idx, Off: e.Offset()}
		default:
			return e, nil
		}
	}
}

func (p *parser) parsePrimary() (Expr, error) {
	t := p.tok
	switch t.kind {
	case tokNumber, tokString:
		if err := p.advance(); err != nil {
			return nil, err
		}
		return &Literal{Value: t.value, Off: t.off}, nil
	case tokLParen:
		if err := p.advance(); err != nil {
			return nil, err
		}
		e, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		if err := p.expect(tokRParen, "')'"); err != nil {
			return nil, err
		}
		return e, nil
	case tokIdent:
		if err := p.advance(); err != nil {
			return nil, err
		}
		switch t.text {
		case "null":
			return &Literal{Value: nil, Off: t.off}, nil
		case "true":
			return &Literal{Value: true, Off: t.off}, nil
		case "false":
			return &Literal{Value: false, Off: t.off}, nil
		}
		if p.tok.kind != tokLParen {
			return &Ident{Name: t.text, Off: t.off}, nil
		}
		return p.parseCall(t)
	}
	return nil, p.unexpected("expression")
}

func (p *parser) parseCall(name token) (Expr, error) {
	if err := p.advance(); err != nil {
		return nil, err
	}
	call := &Call{Name: strings.ToLower(name.text), Off: name.off}
	if p.tok.kind == tokRParen {
		return call, p.advance()
	}
	for {
		arg, err := p.parseExpr()
		if err != nil {
			return nil, err
		}
		call.Args = append(call.Args, arg)
		if p.tok.kind == tokComma {
			if err := p.advance(); err != nil {
				return nil, err
			}
			continue
		}
		if err := p.expect(tokRParen, "',' or ')'"); err != nil {
			return nil, err
		}
		return call, nil
	}
}

func parseRange(src []rune, start, end int, stop tokenKind) (Expr, int, error) {
	p, err := newParser(src, start, end)
	if err != nil {
		return nil, 0, err
	}
	if p.tok.kind == stop {
		return nil, 0, syntaxErrorf(p.tok.off, "empty expression")
	}
	e, err := p.parseExpr()
	if err != nil {
		return nil, 0, err
	}
	if p.tok.kind != stop {
		if stop == tokClose {
			return nil, 0, p.unexpected("'}}'")
		}
		return nil, 0, p.unexpected("end of expression")
	}
	return e, p.lex.pos, nil
}

func ParseExpression(s string) (Expr, error) {
	src := []rune(s)
	e, _, err := parseRange(src, 0, len(src), tokEOF)
	if err != nil {
		return nil, err
	}
	return e, nil
}
