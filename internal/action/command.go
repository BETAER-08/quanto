package action

import "strings"

type prop struct {
	name  string
	value string
}

func command(name string, props []prop, message string) string {
	escape := func(s string, property bool) string {
		var b strings.Builder
		for _, r := range s {
			switch {
			case r == '%':
				b.WriteString("%25")
			case r == '\r':
				b.WriteString("%0D")
			case r == '\n':
				b.WriteString("%0A")
			case property && r == ':':
				b.WriteString("%3A")
			case property && r == ',':
				b.WriteString("%2C")
			default:
				b.WriteRune(r)
			}
		}
		return b.String()
	}
	var b strings.Builder
	b.WriteString("::")
	b.WriteString(name)
	for i, p := range props {
		if i == 0 {
			b.WriteByte(' ')
		} else {
			b.WriteByte(',')
		}
		b.WriteString(p.name)
		b.WriteByte('=')
		b.WriteString(escape(p.value, true))
	}
	b.WriteString("::")
	b.WriteString(escape(message, false))
	return b.String()
}
