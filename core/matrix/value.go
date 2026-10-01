package matrix

import (
	"errors"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/BETAER-08/quanto/core/source"
)

const (
	maxValueDepth = 64
	maxValueNodes = 1000000
)

var errValueBudget = errors.New("matrix values exceed the conversion budget")

type converter struct {
	visited int
}

func (c *converter) value(n *source.Node, depth int) (any, error) {
	if depth > maxValueDepth {
		return nil, errors.New("matrix value nesting exceeds 64 levels")
	}
	c.visited++
	if c.visited > maxValueNodes {
		return nil, errValueBudget
	}
	switch n.Kind() {
	case source.KindMapping:
		fields := n.Fields()
		m := make(map[string]any, len(fields))
		for _, f := range fields {
			v, err := c.value(f.Value, depth+1)
			if err != nil {
				return nil, err
			}
			m[f.Name] = v
		}
		return m, nil
	case source.KindSequence:
		items := n.Items()
		s := make([]any, 0, len(items))
		for _, it := range items {
			v, err := c.value(it, depth+1)
			if err != nil {
				return nil, err
			}
			s = append(s, v)
		}
		return s, nil
	case source.KindScalar:
		return scalarValue(n), nil
	}
	return nil, nil
}

func scalarValue(n *source.Node) any {
	if n.IsNull() {
		return nil
	}
	raw, _ := n.Str()
	switch n.Tag() {
	case "!!int":
		if i, ok := n.Int(); ok {
			return int64(i)
		}
	case "!!float":
		if f, ok := parseYAMLFloat(raw); ok {
			return f
		}
	case "!!bool":
		if b, ok := n.Bool(); ok {
			return b
		}
	}
	return raw
}

func parseYAMLFloat(s string) (float64, bool) {
	switch strings.ToLower(s) {
	case ".inf", "+.inf":
		return math.Inf(1), true
	case "-.inf":
		return math.Inf(-1), true
	case ".nan":
		return math.NaN(), true
	}
	f, err := strconv.ParseFloat(strings.ReplaceAll(s, "_", ""), 64)
	if err != nil {
		return 0, false
	}
	return f, true
}

func canon(v any) string {
	var b strings.Builder
	writeCanon(&b, v)
	return b.String()
}

func writeCanon(b *strings.Builder, v any) {
	switch x := v.(type) {
	case nil:
		b.WriteString("null")
	case bool:
		b.WriteString(strconv.FormatBool(x))
	case int64:
		b.WriteString(strconv.FormatInt(x, 10))
	case float64:
		switch {
		case math.IsNaN(x):
			b.WriteString("NaN")
		case math.IsInf(x, 1):
			b.WriteString("Infinity")
		case math.IsInf(x, -1):
			b.WriteString("-Infinity")
		default:
			b.WriteString(strconv.FormatFloat(x, 'g', -1, 64))
		}
	case string:
		b.WriteString(strconv.Quote(x))
	case []any:
		b.WriteByte('[')
		for i, e := range x {
			if i > 0 {
				b.WriteByte(',')
			}
			writeCanon(b, e)
		}
		b.WriteByte(']')
	case map[string]any:
		keys := make([]string, 0, len(x))
		for k := range x {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		b.WriteByte('{')
		for i, k := range keys {
			if i > 0 {
				b.WriteByte(',')
			}
			b.WriteString(strconv.Quote(k))
			b.WriteByte(':')
			writeCanon(b, x[k])
		}
		b.WriteByte('}')
	}
}
