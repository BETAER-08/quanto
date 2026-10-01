package semdiff

import "testing"

func TestCronRunsPerDay(t *testing.T) {
	cases := []struct {
		expr     string
		runs     int
		everyDay bool
		ok       bool
	}{
		{"0 * * * *", 24, true, true},
		{"*/15 * * * *", 96, true, true},
		{"0 0 * * *", 1, true, true},
		{"0 9 * * 1-5", 1, false, true},
		{"30 2,14 * * *", 2, true, true},
		{"0 9 * * MON", 0, false, false},
		{"0 0 1 JAN *", 0, false, false},
		{"0 * * *", 0, false, false},
		{"0 * * * * *", 0, false, false},
		{"", 0, false, false},
		{"0-29/10 8-17/3 * * *", 12, true, true},
		{"0,0,30 */12 * * *", 4, true, true},
		{"0 0 1 * *", 1, false, true},
		{"0 0 * 6 *", 1, false, true},
		{"60 * * * *", 0, false, false},
		{"0 24 * * *", 0, false, false},
		{"5/10 * * * *", 0, false, false},
		{"*/0 * * * *", 0, false, false},
		{"10-5 * * * *", 0, false, false},
		{"0 * 0 * *", 0, false, false},
		{"0 * * 13 *", 0, false, false},
		{"0 * * * 8", 0, false, false},
		{"-1 * * * *", 0, false, false},
		{"1, * * * *", 0, false, false},
		{"99999999999999999999 * * * *", 0, false, false},
		{"  0   *  * * *  ", 24, true, true},
	}
	for _, c := range cases {
		runs, everyDay, ok := CronRunsPerDay(c.expr)
		if runs != c.runs || everyDay != c.everyDay || ok != c.ok {
			t.Errorf("CronRunsPerDay(%q) = (%d, %v, %v), want (%d, %v, %v)", c.expr, runs, everyDay, ok, c.runs, c.everyDay, c.ok)
		}
	}
}

func TestFormatCron(t *testing.T) {
	cases := map[string]string{
		"0 * * * *":   "'0 * * * *' (24 runs/day)",
		"0 9 * * 1-5": "'0 9 * * 1-5' (1 runs on matching days)",
		"0 9 * * MON": "'0 9 * * MON'",
	}
	for in, want := range cases {
		if got := formatCron(in); got != want {
			t.Errorf("formatCron(%q) = %q, want %q", in, got, want)
		}
	}
	if got := formatCrons(nil); got != "(none)" {
		t.Errorf("formatCrons(nil) = %q", got)
	}
	if got := formatCrons([]string{"0 0 * * *", "0 12 * * *"}); got != "'0 0 * * *' (1 runs/day), '0 12 * * *' (1 runs/day)" {
		t.Errorf("formatCrons = %q", got)
	}
}
