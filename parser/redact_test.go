package parser

import "testing"

func TestRedactMessage(t *testing.T) {
	for in, want := range map[string]string{
		`LOG:  duration: 3 ms  statement: SELECT * FROM t WHERE email = 'a@b.fr' AND n = 'O''Brien'`: `LOG:  duration: 3 ms  statement: SELECT * FROM t WHERE email = '?' AND n = '?'`,
		`DETAIL:  parameters: $1 = 'Dupont', $2 = '75001'`:                                           `DETAIL:  parameters: $1 = '?', $2 = '?'`,
		`DETAIL:  Key (email)=(jean@exemple.fr) already exists.`:                                     `DETAIL:  Key (email)=(?) already exists.`,
		`DETAIL:  Key (a, b)=(1, f(2)) is not present in table "x".`:                                 `DETAIL:  Key (a, b)=(?) is not present in table "x".`,
		`DETAIL:  Failing row contains (12, Jean, null).`:                                            `DETAIL:  Failing row contains (?).`,
		`Filter: ((load_ref)::text = '00091505'::text)`:                                              `Filter: ((load_ref)::text = '?'::text)`,
		`LOG:  statement: SELECT 'unterminated`:                                                      `LOG:  statement: SELECT '?'`,
		`LOG:  checkpoint complete: wrote 12 buffers`:                                                `LOG:  checkpoint complete: wrote 12 buffers`,
	} {
		if got := RedactMessage(in); got != want {
			t.Errorf("RedactMessage(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

func FuzzRedactMessage(f *testing.F) {
	f.Add(`SELECT 'a''b', $$x$$ FROM t WHERE k = 'v'`)
	f.Add(`Key (a)=(b) Failing row contains ((x)`)
	f.Fuzz(func(t *testing.T, s string) {
		r := RedactMessage(s)
		if RedactMessage(r) != r {
			t.Errorf("not idempotent: %q → %q → %q", s, r, RedactMessage(r))
		}
	})
}
