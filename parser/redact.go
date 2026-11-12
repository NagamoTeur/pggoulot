package parser

import (
	"context"
	"strings"
)

// Redaction (fork, --redact): logs carry values that may be personal data —
// query literals, bound parameters, the key values of constraint errors. The
// redaction masks them in every entry before the analysis, so no report, in
// any format, shows them. Numbers outside quotes and dollar-quoted strings
// are left as they are.

// RedactMessage masks the quoted literals ('…' → '?', ” escapes included)
// and the values of "Key (col)=(value)" and "Failing row contains (…)".
func RedactMessage(s string) string {
	if strings.IndexByte(s, '\'') >= 0 {
		s = maskQuoted(s)
	}
	if strings.Contains(s, "Key (") {
		s = maskKeyValues(s)
	}
	if strings.Contains(s, "Failing row contains (") {
		s = maskAfter(s, "Failing row contains (")
	}
	return s
}

// maskQuoted replaces the content of every single-quoted literal with "?".
// An unterminated literal is masked to the end of the message.
func maskQuoted(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		if s[i] != '\'' {
			b.WriteByte(s[i])
			i++
			continue
		}
		b.WriteString("'?'")
		j := i + 1
		for j < len(s) {
			if s[j] == '\'' {
				if j+1 < len(s) && s[j+1] == '\'' {
					j += 2 // doubled quote inside the literal
					continue
				}
				break
			}
			j++
		}
		i = j + 1
	}
	return b.String()
}

// closing returns the index of the parenthesis closing the one opened right
// before s[from], or -1.
func closing(s string, from int) int {
	depth := 1
	for i := from; i < len(s); i++ {
		switch s[i] {
		case '(':
			depth++
		case ')':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// maskAfter replaces what each "marker(" … ")" encloses with "?"; an
// unclosed one is masked to the end of the message.
func maskAfter(s, marker string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, marker)
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		start := i + len(marker)
		b.WriteString(s[:start])
		b.WriteString("?")
		end := closing(s, start)
		if end < 0 {
			b.WriteString(")")
			return b.String()
		}
		s = s[end:]
	}
}

// maskKeyValues masks the values of "Key (columns)=(values)", whatever
// parentheses the columns and the values hold.
func maskKeyValues(s string) string {
	var b strings.Builder
	for {
		i := strings.Index(s, "Key (")
		if i < 0 {
			b.WriteString(s)
			return b.String()
		}
		cols := closing(s, i+len("Key ("))
		if cols < 0 || !strings.HasPrefix(s[cols:], ")=(") {
			b.WriteString(s[:i+len("Key (")])
			s = s[i+len("Key ("):]
			continue
		}
		start := cols + len(")=(")
		b.WriteString(s[:start])
		b.WriteString("?")
		end := closing(s, start)
		if end < 0 {
			b.WriteString(")")
			return b.String()
		}
		s = s[end:]
	}
}

// RedactStream applies RedactMessage to every entry between in and out.
func RedactStream(ctx context.Context, in <-chan []LogEntry, out chan<- []LogEntry) {
	defer close(out)
	for batch := range in {
		for i := range batch {
			batch[i].Message = RedactMessage(batch[i].Message)
		}
		select {
		case out <- batch:
		case <-ctx.Done():
			for range in {
			}
			return
		}
	}
}
