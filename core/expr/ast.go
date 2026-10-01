package expr

type Expr interface{ Offset() int }

type Literal struct {
	Value any
	Off   int
}

type Ident struct {
	Name string
	Off  int
}

type Property struct {
	Target Expr
	Name   string
	Off    int
}

type Index struct {
	Target Expr
	Index  Expr
	Off    int
}

type Star struct {
	Target Expr
	Off    int
}

type Unary struct {
	Op  string
	X   Expr
	Off int
}

type Binary struct {
	Op          string
	Left, Right Expr
	Off         int
}

type Call struct {
	Name string
	Args []Expr
	Off  int
}

func (e *Literal) Offset() int  { return e.Off }
func (e *Ident) Offset() int    { return e.Off }
func (e *Property) Offset() int { return e.Off }
func (e *Index) Offset() int    { return e.Off }
func (e *Star) Offset() int     { return e.Off }
func (e *Unary) Offset() int    { return e.Off }
func (e *Binary) Offset() int   { return e.Off }
func (e *Call) Offset() int     { return e.Off }

type Segment struct {
	Text   string
	Expr   Expr
	IsExpr bool
	Offset int
}

type Template struct {
	Segments []Segment
}

type Reference struct {
	Context string
	Path    []string
}
