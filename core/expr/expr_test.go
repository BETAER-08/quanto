package expr

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func show(e Expr) string {
	switch x := e.(type) {
	case *Literal:
		switch v := x.Value.(type) {
		case nil:
			return "null"
		case string:
			return "'" + strings.ReplaceAll(v, "'", "''") + "'"
		case bool:
			if v {
				return "true"
			}
			return "false"
		case float64:
			return strconv.FormatFloat(v, 'f', -1, 64)
		}
	case *Ident:
		return x.Name
	case *Property:
		return show(x.Target) + "." + x.Name
	case *Index:
		return show(x.Target) + "[" + show(x.Index) + "]"
	case *Star:
		return show(x.Target) + ".*"
	case *Unary:
		return "(" + x.Op + show(x.X) + ")"
	case *Binary:
		return "(" + show(x.Left) + " " + x.Op + " " + show(x.Right) + ")"
	case *Call:
		args := make([]string, len(x.Args))
		for i, a := range x.Args {
			args[i] = show(a)
		}
		return x.Name + "(" + strings.Join(args, ", ") + ")"
	}
	return "?"
}

func TestParseExpression(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"a || b && c", "(a || (b && c))"},
		{"a && b || c", "((a && b) || c)"},
		{"!a == b", "((!a) == b)"},
		{"!!a", "(!(!a))"},
		{"a == b && c != d", "((a == b) && (c != d))"},
		{"a < b == c >= d", "((a < b) == (c >= d))"},
		{"a <= b > c", "((a <= b) > c)"},
		{"(a || b) && c", "((a || b) && c)"},
		{"github.event.pull_request.head.sha", "github.event.pull_request.head.sha"},
		{"a.b[0].c['d']", "a.b[0].c['d']"},
		{"secrets['MY_TOKEN']", "secrets['MY_TOKEN']"},
		{"fromJSON(needs.setup.outputs.matrix)", "fromjson(needs.setup.outputs.matrix)"},
		{"contains(github.event.issue.labels.*.name, 'bug')", "contains(github.event.issue.labels.*.name, 'bug')"},
		{"Format('{0}', a)", "format('{0}', a)"},
		{"success()", "success()"},
		{"myFunc(1, 'x', null, true, false)", "myfunc(1, 'x', null, true, false)"},
		{"fromJSON(x).include[0]", "fromjson(x).include[0]"},
		{"'it''s'", "'it''s'"},
		{"0xff", "255"},
		{"0XFF", "255"},
		{"1e3", "1000"},
		{"-2.5E-1", "-0.25"},
		{"-12", "-12"},
		{"steps.build-x.outputs.my_out", "steps.build-x.outputs.my_out"},
		{"a.true", "a.true"},
		{"  a  ", "a"},
	}
	for _, tt := range tests {
		e, err := ParseExpression(tt.in)
		if err != nil {
			t.Errorf("ParseExpression(%q): %v", tt.in, err)
			continue
		}
		if got := show(e); got != tt.want {
			t.Errorf("ParseExpression(%q) = %s, want %s", tt.in, got, tt.want)
		}
	}
}

func TestParseExpressionErrors(t *testing.T) {
	tests := []struct {
		in  string
		off int
	}{
		{"", 0},
		{"a ||", 4},
		{"a &", 2},
		{"a = b", 2},
		{"'abc", 0},
		{"a.", 2},
		{"a[1", 3},
		{"f(a,", 4},
		{"(a", 2},
		{"a b", 2},
		{"0x", 0},
		{"1.", 0},
		{"01", 0},
		{"1e", 0},
		{".5", 0},
		{"a }}", 2},
		{"한 == a", 0},
		{"a == 한", 5},
	}
	for _, tt := range tests {
		_, err := ParseExpression(tt.in)
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("ParseExpression(%q) error = %v, want *SyntaxError", tt.in, err)
			continue
		}
		if se.Offset != tt.off {
			t.Errorf("ParseExpression(%q) offset = %d, want %d (%s)", tt.in, se.Offset, tt.off, se.Message)
		}
		if se.Error() == "" {
			t.Errorf("empty error text for %q", tt.in)
		}
	}
}

func TestLiteralValues(t *testing.T) {
	tests := []struct {
		in   string
		want any
	}{
		{"null", nil},
		{"true", true},
		{"false", false},
		{"0", float64(0)},
		{"42", float64(42)},
		{"3.14", 3.14},
		{"1E+2", float64(100)},
		{"0x1F", float64(31)},
		{"-0x10", float64(-16)},
		{"'a''b'", "a'b"},
		{"''", ""},
		{"'}}'", "}}"},
	}
	for _, tt := range tests {
		e, err := ParseExpression(tt.in)
		if err != nil {
			t.Errorf("ParseExpression(%q): %v", tt.in, err)
			continue
		}
		lit, ok := e.(*Literal)
		if !ok {
			t.Errorf("ParseExpression(%q) = %T, want *Literal", tt.in, e)
			continue
		}
		if !reflect.DeepEqual(lit.Value, tt.want) {
			t.Errorf("ParseExpression(%q) value = %#v, want %#v", tt.in, lit.Value, tt.want)
		}
	}
}

func TestOffsetsAreRunes(t *testing.T) {
	s := "'한글' == github.actor"
	e, err := ParseExpression(s)
	if err != nil {
		t.Fatal(err)
	}
	b := e.(*Binary)
	if b.Offset() != 0 {
		t.Errorf("binary offset = %d", b.Offset())
	}
	right := b.Right.(*Property)
	if right.Offset() != 8 {
		t.Errorf("right offset = %d, want 8", right.Offset())
	}
	if id := right.Target.(*Ident); id.Off != 8 {
		t.Errorf("ident offset = %d, want 8", id.Off)
	}
	tpl, err := ParseTemplate("가나 ${{ x }}")
	if err != nil {
		t.Fatal(err)
	}
	if tpl.Segments[1].Offset != 3 || tpl.Segments[1].Expr.Offset() != 7 {
		t.Errorf("template offsets = %d, %d", tpl.Segments[1].Offset, tpl.Segments[1].Expr.Offset())
	}
}

func TestNodeOffsets(t *testing.T) {
	e, err := ParseExpression("!f(a.*, b[c]) || d")
	if err != nil {
		t.Fatal(err)
	}
	or := e.(*Binary)
	not := or.Left.(*Unary)
	call := not.X.(*Call)
	star := call.Args[0].(*Star)
	idx := call.Args[1].(*Index)
	got := []int{or.Off, not.Off, call.Off, star.Off, idx.Off, idx.Index.Offset(), or.Right.Offset()}
	want := []int{0, 0, 1, 3, 8, 10, 17}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("offsets = %v, want %v", got, want)
	}
}

type segView struct {
	Text   string
	Expr   string
	IsExpr bool
	Offset int
}

func viewTemplate(tpl *Template) []segView {
	var out []segView
	for _, s := range tpl.Segments {
		v := segView{Text: s.Text, IsExpr: s.IsExpr, Offset: s.Offset}
		if s.IsExpr {
			v.Expr = show(s.Expr)
		}
		out = append(out, v)
	}
	return out
}

func TestParseTemplate(t *testing.T) {
	tests := []struct {
		in   string
		want []segView
	}{
		{"", nil},
		{"plain text", []segView{{Text: "plain text"}}},
		{"${{ a }}", []segView{{Text: "${{ a }}", Expr: "a", IsExpr: true}}},
		{"prefix ${{ a }} mid ${{ b }}", []segView{
			{Text: "prefix "},
			{Text: "${{ a }}", Expr: "a", IsExpr: true, Offset: 7},
			{Text: " mid ", Offset: 15},
			{Text: "${{ b }}", Expr: "b", IsExpr: true, Offset: 20},
		}},
		{"${{ format('}}', a) }} tail", []segView{
			{Text: "${{ format('}}', a) }}", Expr: "format('}}', a)", IsExpr: true},
			{Text: " tail", Offset: 22},
		}},
		{"${{ '${{ x }}' }}", []segView{{Text: "${{ '${{ x }}' }}", Expr: "'${{ x }}'", IsExpr: true}}},
		{"${{a}}${{b}}", []segView{
			{Text: "${{a}}", Expr: "a", IsExpr: true},
			{Text: "${{b}}", Expr: "b", IsExpr: true, Offset: 6},
		}},
		{"a } b }} c", []segView{{Text: "a } b }} c"}}},
		{"${{ matrix.os }}-${{ matrix.node }}", []segView{
			{Text: "${{ matrix.os }}", Expr: "matrix.os", IsExpr: true},
			{Text: "-", Offset: 16},
			{Text: "${{ matrix.node }}", Expr: "matrix.node", IsExpr: true, Offset: 17},
		}},
	}
	for _, tt := range tests {
		tpl, err := ParseTemplate(tt.in)
		if err != nil {
			t.Errorf("ParseTemplate(%q): %v", tt.in, err)
			continue
		}
		if got := viewTemplate(tpl); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseTemplate(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
		var joined strings.Builder
		for _, s := range tpl.Segments {
			joined.WriteString(s.Text)
		}
		if joined.String() != tt.in {
			t.Errorf("segments of %q join to %q", tt.in, joined.String())
		}
	}
}

func TestParseTemplateErrors(t *testing.T) {
	tests := []struct {
		in  string
		off int
		msg string
	}{
		{"${{ a", 0, "unclosed expression"},
		{"x ${{ a }", 8, ""},
		{"ok ${{ a }} then ${{ b", 17, "unclosed expression"},
		{"${{ '}} }}", 4, "unterminated string literal"},
		{"${{ }}", 4, "empty expression"},
		{"${{ a b }}", 6, ""},
		{"${{ a } }}", 6, ""},
	}
	for _, tt := range tests {
		_, err := ParseTemplate(tt.in)
		var se *SyntaxError
		if !errors.As(err, &se) {
			t.Errorf("ParseTemplate(%q) error = %v, want *SyntaxError", tt.in, err)
			continue
		}
		if se.Offset != tt.off {
			t.Errorf("ParseTemplate(%q) offset = %d, want %d", tt.in, se.Offset, tt.off)
		}
		if tt.msg != "" && se.Message != tt.msg {
			t.Errorf("ParseTemplate(%q) message = %q, want %q", tt.in, se.Message, tt.msg)
		}
	}
}

func TestParseCondition(t *testing.T) {
	tests := []struct {
		in   string
		want []segView
	}{
		{"github.event_name == 'push'", []segView{{Text: "github.event_name == 'push'", Expr: "(github.event_name == 'push')", IsExpr: true}}},
		{"  ${{ success() && a }}  ", []segView{{Text: "${{ success() && a }}", Expr: "(success() && a)", IsExpr: true, Offset: 2}}},
		{"${{ a }}", []segView{{Text: "${{ a }}", Expr: "a", IsExpr: true}}},
		{"${{ a }} && ${{ b }}", []segView{
			{Text: "${{ a }}", Expr: "a", IsExpr: true},
			{Text: " && ", Offset: 8},
			{Text: "${{ b }}", Expr: "b", IsExpr: true, Offset: 12},
		}},
		{"x ${{ a }}", []segView{
			{Text: "x "},
			{Text: "${{ a }}", Expr: "a", IsExpr: true, Offset: 2},
		}},
	}
	for _, tt := range tests {
		tpl, err := ParseCondition(tt.in)
		if err != nil {
			t.Errorf("ParseCondition(%q): %v", tt.in, err)
			continue
		}
		if got := viewTemplate(tpl); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("ParseCondition(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
	for _, bad := range []string{"a ==", "${{ a", "", "${{ a }} ${{"} {
		if _, err := ParseCondition(bad); err == nil {
			t.Errorf("ParseCondition(%q) succeeded", bad)
		}
	}
}

func TestReferences(t *testing.T) {
	tests := []struct {
		in   string
		want []Reference
	}{
		{"github.event.pull_request.head.sha", []Reference{{"github", []string{"event", "pull_request", "head", "sha"}}}},
		{"GitHub.Event_Name", []Reference{{"github", []string{"event_name"}}}},
		{"secrets['MY_TOKEN']", []Reference{{"secrets", []string{"my_token"}}}},
		{"secrets.MY_TOKEN || secrets['my_token']", []Reference{{"secrets", []string{"my_token"}}}},
		{"fromJSON(needs.setup.outputs.matrix)", []Reference{{"needs", []string{"setup", "outputs", "matrix"}}}},
		{"github.event.issue.labels.*.name", []Reference{{"github", []string{"event", "issue", "labels", "*", "name"}}}},
		{"matrix[inputs.key].x", []Reference{{"inputs", []string{"key"}}, {"matrix", []string{"*", "x"}}}},
		{"a[0]", []Reference{{"a", []string{"*"}}}},
		{"runner", []Reference{{"runner", nil}}},
		{"fromJSON(x).include[0].os", []Reference{{"x", nil}}},
		{"b.y && a.x || !c", []Reference{{"a", []string{"x"}}, {"b", []string{"y"}}, {"c", nil}}},
		{"a.b && a && a.b.c && a.c", []Reference{{"a", nil}, {"a", []string{"b"}}, {"a", []string{"b", "c"}}, {"a", []string{"c"}}}},
		{"'x' == 1", nil},
	}
	for _, tt := range tests {
		e, err := ParseExpression(tt.in)
		if err != nil {
			t.Errorf("ParseExpression(%q): %v", tt.in, err)
			continue
		}
		if got := References(e); !reflect.DeepEqual(got, tt.want) {
			t.Errorf("References(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestTemplateReferences(t *testing.T) {
	tpl, err := ParseTemplate("${{ secrets.B }} and ${{ secrets.A }} and ${{ secrets.b }}")
	if err != nil {
		t.Fatal(err)
	}
	want := []Reference{{"secrets", []string{"a"}}, {"secrets", []string{"b"}}}
	if got := TemplateReferences(tpl); !reflect.DeepEqual(got, want) {
		t.Errorf("TemplateReferences = %v, want %v", got, want)
	}
	if got := TemplateReferences(nil); got != nil {
		t.Errorf("TemplateReferences(nil) = %v", got)
	}
}

func TestFunctions(t *testing.T) {
	e, err := ParseExpression("Contains(toJSON(a), 'x') && success() || contains(b[format('{0}', c)], 1)")
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"contains", "format", "success", "tojson"}
	if got := Functions(e); !reflect.DeepEqual(got, want) {
		t.Errorf("Functions = %v, want %v", got, want)
	}
	plain, err := ParseExpression("a.b")
	if err != nil {
		t.Fatal(err)
	}
	if got := Functions(plain); got != nil {
		t.Errorf("Functions(a.b) = %v", got)
	}
}

func TestIsDynamic(t *testing.T) {
	tests := map[string]bool{
		"":               false,
		"ubuntu-latest":  false,
		"${{ x }}":       true,
		"pre-${{ x }}":   true,
		"${ { x } }":     false,
		"$${{":           true,
		"{{ not ours }}": false,
	}
	for in, want := range tests {
		if got := IsDynamic(in); got != want {
			t.Errorf("IsDynamic(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestDeterminism(t *testing.T) {
	in := "${{ z.a || y['b'] && x.*.c }} ${{ fromJSON(w).v }} ${{ secrets.T }}"
	first, err := ParseTemplate(in)
	if err != nil {
		t.Fatal(err)
	}
	want := TemplateReferences(first)
	for i := 0; i < 50; i++ {
		tpl, err := ParseTemplate(in)
		if err != nil {
			t.Fatal(err)
		}
		if got := TemplateReferences(tpl); !reflect.DeepEqual(got, want) {
			t.Fatalf("iteration %d: %v != %v", i, got, want)
		}
	}
}

func TestDeepNesting(t *testing.T) {
	deep := strings.Repeat("(", 5000) + "a" + strings.Repeat(")", 5000)
	if _, err := ParseExpression(deep); err == nil {
		t.Error("deeply nested expression accepted")
	}
	nots := strings.Repeat("!", 5000) + "a"
	if _, err := ParseExpression(nots); err == nil {
		t.Error("deeply nested negation accepted")
	}
	chain := "a" + strings.Repeat(".b", 5000)
	e, err := ParseExpression(chain)
	if err != nil {
		t.Fatal(err)
	}
	refs := References(e)
	if len(refs) != 1 || len(refs[0].Path) != 5000 {
		t.Errorf("long chain references = %d", len(refs))
	}
}

func FuzzParseTemplate(f *testing.F) {
	seeds := []string{
		"",
		"plain",
		"${{ a }}",
		"prefix ${{ a || b && !c }} mid ${{ fromJSON(x).y[0] }}",
		"${{ format('}}', a) }}",
		"${{ a",
		"${{ github.event.issue.labels.*.name }}",
		"${{ 0xff == 1e3 }}",
		"${{ '한글' }}",
		"${{ secrets['MY_TOKEN'] }}",
	}
	for _, s := range seeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		tpl, err := ParseTemplate(s)
		if err != nil {
			var se *SyntaxError
			if !errors.As(err, &se) {
				t.Fatalf("non-syntax error %T: %v", err, err)
			}
			return
		}
		var joined strings.Builder
		for _, seg := range tpl.Segments {
			joined.WriteString(seg.Text)
			if seg.IsExpr {
				Functions(seg.Expr)
			}
		}
		if joined.String() != string([]rune(s)) {
			t.Fatalf("segments do not reconstruct input")
		}
		refs := TemplateReferences(tpl)
		for i := 1; i < len(refs); i++ {
			if compareRefs(refs[i-1], refs[i]) >= 0 {
				t.Fatalf("references not sorted and unique: %v", refs)
			}
		}
		cond, err := ParseCondition(s)
		if err == nil && cond == nil {
			t.Fatalf("nil condition without error")
		}
	})
}
