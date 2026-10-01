package matrix

import (
	"fmt"
	"strconv"

	"github.com/BETAER-08/quanto/core/expr"
	"github.com/BETAER-08/quanto/core/source"
)

const GitHubJobLimit = 256
const MaterializeLimit = 100000

const (
	codeEmptyAxis = "MATRIX-EMPTY-AXIS"
	codeEmpty     = "MATRIX-EMPTY"
	codeTooLarge  = "MATRIX-TOO-LARGE"
	codeOverLimit = "MATRIX-OVER-LIMIT"
)

type Instance struct {
	Values      map[string]any
	FromInclude bool
}

type Diagnostic struct {
	Code    string
	Message string
	Pos     source.Position
}

type Expansion struct {
	Axes          []string
	Instances     []Instance
	Count         int
	Materialized  bool
	Dynamic       bool
	DynamicReason string
	Diagnostics   []Diagnostic
}

type axis struct {
	name   string
	values []any
	canons []string
	pos    source.Position
}

type entry struct {
	key   string
	value any
	canon string
}

type object struct {
	entries []entry
}

type combo struct {
	index  []int
	extras map[string]any
}

func Expand(matrix *source.Node) (*Expansion, error) {
	if matrix == nil {
		return &Expansion{Count: 1, Instances: []Instance{{Values: map[string]any{}}}, Materialized: true}, nil
	}
	if reason, ok := dynamicReason(matrix); ok {
		return &Expansion{Axes: axisNames(matrix), Dynamic: true, DynamicReason: reason}, nil
	}
	if matrix.Kind() != source.KindMapping {
		return nil, fmt.Errorf("%s: strategy.matrix must be a mapping", matrix.Pos())
	}
	conv := &converter{}
	var axes []axis
	var includeNode, excludeNode *source.Node
	for _, f := range matrix.Fields() {
		switch f.Name {
		case "include":
			includeNode = f.Value
			continue
		case "exclude":
			excludeNode = f.Value
			continue
		}
		if f.Value.Kind() != source.KindSequence {
			return nil, fmt.Errorf("%s: matrix axis %q must be a sequence", f.Value.Pos(), f.Name)
		}
		a := axis{name: f.Name, pos: f.Key.Pos()}
		for _, it := range f.Value.Items() {
			v, err := conv.value(it, 1)
			if err != nil {
				return nil, fmt.Errorf("%s: matrix axis %q: %w", it.Pos(), f.Name, err)
			}
			a.values = append(a.values, v)
			a.canons = append(a.canons, canon(v))
		}
		axes = append(axes, a)
	}
	includes, err := objects(conv, includeNode, "include")
	if err != nil {
		return nil, err
	}
	excludes, err := objects(conv, excludeNode, "exclude")
	if err != nil {
		return nil, err
	}

	exp := &Expansion{Axes: make([]string, 0, len(axes))}
	for _, a := range axes {
		exp.Axes = append(exp.Axes, a.name)
	}
	if len(axes) == 0 && len(includes) == 0 {
		exp.Diagnostics = append(exp.Diagnostics, Diagnostic{
			Code:    codeEmpty,
			Message: "matrix defines no axes and no include entries",
			Pos:     matrix.Pos(),
		})
		exp.Materialized = true
		return exp, nil
	}

	product := 0
	if len(axes) > 0 {
		product = 1
	}
	for _, a := range axes {
		if len(a.values) == 0 {
			exp.Diagnostics = append(exp.Diagnostics, Diagnostic{
				Code:    codeEmptyAxis,
				Message: fmt.Sprintf("matrix axis `%s` has no values", a.name),
				Pos:     a.pos,
			})
			product = 0
		}
	}
	if product > 0 {
		for _, a := range axes {
			product *= len(a.values)
			if product > MaterializeLimit {
				break
			}
		}
	}
	if product > MaterializeLimit {
		exp.Count = product
		exp.Diagnostics = append(exp.Diagnostics, Diagnostic{
			Code:    codeTooLarge,
			Message: fmt.Sprintf("matrix has at least %d base combinations; not materialized", product),
			Pos:     matrix.Pos(),
		})
		exp.Diagnostics = append(exp.Diagnostics, overLimit(product, matrix.Pos()))
		return exp, nil
	}

	axisIndex := make(map[string]int, len(axes))
	for i, a := range axes {
		axisIndex[a.name] = i
	}
	combos := baseCombos(axes, product)
	combos = applyExcludes(combos, axes, axisIndex, excludes)
	baseCount := len(combos)
	for _, inc := range includes {
		matched := false
		for ci := 0; ci < baseCount; ci++ {
			c := &combos[ci]
			if !compatible(c, axes, axisIndex, inc) {
				continue
			}
			matched = true
			for _, e := range inc.entries {
				if _, isAxis := axisIndex[e.key]; isAxis {
					continue
				}
				if c.extras == nil {
					c.extras = make(map[string]any)
				}
				c.extras[e.key] = e.value
			}
		}
		if !matched {
			extras := make(map[string]any, len(inc.entries))
			for _, e := range inc.entries {
				extras[e.key] = e.value
			}
			combos = append(combos, combo{extras: extras})
		}
	}

	exp.Instances = make([]Instance, 0, len(combos))
	for i, c := range combos {
		values := make(map[string]any, len(axes)+len(c.extras))
		if c.index != nil {
			for ai, a := range axes {
				values[a.name] = a.values[c.index[ai]]
			}
		}
		for k, v := range c.extras {
			values[k] = v
		}
		exp.Instances = append(exp.Instances, Instance{Values: values, FromInclude: i >= baseCount})
	}
	exp.Count = len(exp.Instances)
	exp.Materialized = true
	if exp.Count > GitHubJobLimit {
		exp.Diagnostics = append(exp.Diagnostics, overLimit(exp.Count, matrix.Pos()))
	}
	return exp, nil
}

func overLimit(count int, pos source.Position) Diagnostic {
	return Diagnostic{
		Code:    codeOverLimit,
		Message: "matrix expands to " + strconv.Itoa(count) + " jobs; GitHub limit is " + strconv.Itoa(GitHubJobLimit),
		Pos:     pos,
	}
}

func dynamicScalar(n *source.Node) bool {
	s, ok := n.Str()
	return ok && expr.IsDynamic(s)
}

func dynamicReason(matrix *source.Node) (string, bool) {
	if dynamicScalar(matrix) {
		return "matrix is an expression", true
	}
	for _, f := range matrix.Fields() {
		if !dynamicScalar(f.Value) {
			continue
		}
		switch f.Name {
		case "include", "exclude":
			return "matrix " + f.Name + " is an expression", true
		}
		return "matrix axis `" + f.Name + "` is an expression", true
	}
	return "", false
}

func axisNames(matrix *source.Node) []string {
	var out []string
	for _, f := range matrix.Fields() {
		if f.Name != "include" && f.Name != "exclude" {
			out = append(out, f.Name)
		}
	}
	return out
}

func objects(conv *converter, n *source.Node, name string) ([]object, error) {
	if n == nil || n.IsNull() {
		return nil, nil
	}
	if n.Kind() != source.KindSequence {
		return nil, fmt.Errorf("%s: matrix %s must be a sequence", n.Pos(), name)
	}
	var out []object
	for _, it := range n.Items() {
		if it.Kind() != source.KindMapping {
			return nil, fmt.Errorf("%s: matrix %s entry must be a mapping", it.Pos(), name)
		}
		var o object
		for _, f := range it.Fields() {
			v, err := conv.value(f.Value, 2)
			if err != nil {
				return nil, fmt.Errorf("%s: matrix %s: %w", f.Value.Pos(), name, err)
			}
			o.entries = append(o.entries, entry{key: f.Name, value: v, canon: canon(v)})
		}
		out = append(out, o)
	}
	return out, nil
}

func baseCombos(axes []axis, product int) []combo {
	if product == 0 {
		return nil
	}
	out := make([]combo, 0, product)
	idx := make([]int, len(axes))
	for {
		cur := make([]int, len(idx))
		copy(cur, idx)
		out = append(out, combo{index: cur})
		i := len(idx) - 1
		for i >= 0 {
			idx[i]++
			if idx[i] < len(axes[i].values) {
				break
			}
			idx[i] = 0
			i--
		}
		if i < 0 {
			return out
		}
	}
}

func matchesAxes(c combo, axes []axis, axisIndex map[string]int, o object) bool {
	for _, e := range o.entries {
		ai, ok := axisIndex[e.key]
		if !ok {
			return false
		}
		if axes[ai].canons[c.index[ai]] != e.canon {
			return false
		}
	}
	return true
}

func applyExcludes(combos []combo, axes []axis, axisIndex map[string]int, excludes []object) []combo {
	if len(excludes) == 0 {
		return combos
	}
	out := combos[:0]
	for _, c := range combos {
		excluded := false
		for _, ex := range excludes {
			if matchesAxes(c, axes, axisIndex, ex) {
				excluded = true
				break
			}
		}
		if !excluded {
			out = append(out, c)
		}
	}
	return out
}

func compatible(c *combo, axes []axis, axisIndex map[string]int, o object) bool {
	for _, e := range o.entries {
		ai, ok := axisIndex[e.key]
		if !ok {
			continue
		}
		if axes[ai].canons[c.index[ai]] != e.canon {
			return false
		}
	}
	return true
}
