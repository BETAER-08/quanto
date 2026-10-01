package action

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func unescape(s string) string {
	r := strings.NewReplacer("%0D", "\r", "%0A", "\n", "%3A", ":", "%2C", ",", "%25", "%")
	return r.Replace(s)
}

type parsedCommand struct {
	name    string
	props   []prop
	message string
}

func parseCommand(t *testing.T, line string) parsedCommand {
	t.Helper()
	rest, ok := strings.CutPrefix(line, "::")
	if !ok {
		t.Fatalf("no command prefix: %q", line)
	}
	head, message, ok := strings.Cut(rest, "::")
	if !ok {
		t.Fatalf("no message separator: %q", line)
	}
	out := parsedCommand{message: unescape(message)}
	name, attrs, hasAttrs := strings.Cut(head, " ")
	out.name = name
	if hasAttrs {
		for _, kv := range strings.Split(attrs, ",") {
			k, v, ok := strings.Cut(kv, "=")
			if !ok {
				t.Fatalf("bad property %q in %q", kv, line)
			}
			out.props = append(out.props, prop{name: k, value: unescape(v)})
		}
	}
	return out
}

func TestCommandEscaping(t *testing.T) {
	tests := []struct {
		name    string
		props   []prop
		message string
		want    string
	}{
		{"plain", nil, "hello", "::notice::hello"},
		{"percent", nil, "100% done %0A", "::notice::100%25 done %250A"},
		{"newline set-output", nil, "x\n::set-output name=a::b", "::notice::x%0A::set-output name=a::b"},
		{"carriage return add-mask", nil, "x\r\n::add-mask::secret", "::notice::x%0D%0A::add-mask::secret"},
		{"stop-commands", nil, "\n::stop-commands::pause\n", "::notice::%0A::stop-commands::pause%0A"},
		{"message keeps colon and comma", nil, "a: b, c", "::notice::a: b, c"},
		{"property delimiters", []prop{{name: "file", value: "a,b:c"}, {name: "title", value: "t\nx=1"}}, "m", "::notice file=a%2Cb%3Ac,title=t%0Ax=1::m"},
		{"property injection", []prop{{name: "title", value: "x::warning::y,file=z"}}, "m", "::notice title=x%3A%3Awarning%3A%3Ay%2Cfile=z::m"},
		{"empty", []prop{{name: "title", value: ""}}, "", "::notice title=::"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := command("notice", tt.props, tt.message)
			if got != tt.want {
				t.Fatalf("command = %q, want %q", got, tt.want)
			}
			if strings.ContainsAny(got, "\r\n") {
				t.Fatalf("command spans lines: %q", got)
			}
			p := parseCommand(t, got)
			if p.message != tt.message || len(p.props) != len(tt.props) {
				t.Fatalf("parsed = %+v", p)
			}
			for i := range tt.props {
				if p.props[i] != tt.props[i] {
					t.Fatalf("prop %d = %+v, want %+v", i, p.props[i], tt.props[i])
				}
			}
		})
	}
}

func FuzzCommand(f *testing.F) {
	f.Add(".github/workflows/ci.yml", "matrix.count_changed", "Job `test` matrix: 6 → 24 jobs")
	f.Add("a\n::add-mask::x", "t,file=y", "m\r\n::set-output name=a::b")
	f.Add("%0A%25", "::stop-commands::z", "\n::stop-commands::tok\n")
	f.Add("", "", "")
	f.Fuzz(func(t *testing.T, file, title, message string) {
		if !utf8.ValidString(file) || !utf8.ValidString(title) || !utf8.ValidString(message) {
			return
		}
		props := []prop{{name: "file", value: file}, {name: "line", value: "1"}, {name: "title", value: title}}
		got := command("notice", props, message)
		if strings.ContainsAny(got, "\r\n") {
			t.Fatalf("command spans lines: %q", got)
		}
		p := parseCommand(t, got)
		if p.name != "notice" || p.message != message || len(p.props) != len(props) {
			t.Fatalf("parsed = %+v from %q", p, got)
		}
		for i := range props {
			if p.props[i] != props[i] {
				t.Fatalf("prop %d = %+v, want %+v", i, p.props[i], props[i])
			}
		}
	})
}
