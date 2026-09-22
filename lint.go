package db

import (
	"fmt"
	"regexp"
	"strings"
)

// Phase classifies a migration within the expand/contract cycle.
//
// The cycle is: Expand (add new structures alongside the old; the old code
// keeps working), Cutover (repoint contract views at the new structures),
// Contract (remove what nothing reads any more). Lint enforces that Expand
// and Cutover migrations contain no destructive DDL, so a botched deploy can
// always fall back to the previous release against the same database.
type Phase int

const (
	// Expand migrations are additive only: new tables, columns, indexes,
	// views. Nothing may be dropped, renamed, retyped or truncated.
	Expand Phase = iota + 1
	// Cutover migrations swap contract views onto new structures. They may
	// drop and recreate views, but nothing else destructive.
	Cutover
	// Contract migrations remove deprecated structures. Anything goes.
	Contract
)

func (p Phase) String() string {
	switch p {
	case Expand:
		return "expand"
	case Cutover:
		return "cutover"
	case Contract:
		return "contract"
	}
	return fmt.Sprintf("Phase(%d)", int(p))
}

// LintError reports a destructive operation found in a migration whose
// phase does not allow it.
type LintError struct {
	Phase     Phase
	Operation string // e.g. "DROP TABLE", "RENAME", "ALTER COLUMN TYPE"
	Statement string // the offending statement, trimmed
}

func (e *LintError) Error() string {
	return fmt.Sprintf("db: %s migration contains %s: %s", e.Phase, e.Operation, oneLine(e.Statement, 80))
}

var destructiveOps = []struct {
	name string
	re   *regexp.Regexp
}{
	{"DROP VIEW", regexp.MustCompile(`\bDROP\s+(MATERIALIZED\s+)?VIEW\b`)},
	{"DROP", regexp.MustCompile(`\bDROP\s+(TABLE|COLUMN|INDEX|SCHEMA|SEQUENCE|TRIGGER|FUNCTION|TYPE|DOMAIN|CONSTRAINT)\b`)},
	{"RENAME", regexp.MustCompile(`\bRENAME\b`)},
	{"ALTER COLUMN TYPE", regexp.MustCompile(`\bALTER\s+(COLUMN\s+)?\S+\s+(SET\s+DATA\s+)?TYPE\b`)},
	{"TRUNCATE", regexp.MustCompile(`\bTRUNCATE\b`)},
	{"DELETE", regexp.MustCompile(`^\s*DELETE\b`)},
}

// Lint checks that script contains only operations allowed in phase.
// Expand forbids every destructive operation; Cutover additionally allows
// DROP VIEW (so a contract view can be dropped and recreated in one
// transaction); Contract allows everything.
func Lint(phase Phase, script string) error {
	if phase == Contract {
		return nil
	}
	for _, stmt := range SplitStatements(script) {
		bare := strings.ToUpper(stripLiterals(stmt))
		for _, op := range destructiveOps {
			if op.name == "DROP VIEW" && phase == Cutover {
				continue
			}
			if op.re.MatchString(bare) {
				return &LintError{Phase: phase, Operation: op.name, Statement: stmt}
			}
		}
	}
	return nil
}

// stripLiterals blanks out string literals, quoted identifiers, dollar-quoted
// blocks and comments so keyword matching cannot be fooled by their contents.
func stripLiterals(s string) string {
	var b strings.Builder
	n := len(s)
	for i := 0; i < n; {
		c := s[i]
		switch {
		case c == '-' && i+1 < n && s[i+1] == '-':
			j := strings.IndexByte(s[i:], '\n')
			if j < 0 {
				return b.String()
			}
			i += j
		case c == '/' && i+1 < n && s[i+1] == '*':
			j := strings.Index(s[i+2:], "*/")
			if j < 0 {
				return b.String()
			}
			i += j + 4
			b.WriteByte(' ')
		case c == '\'' || c == '"':
			j := i + 1
			for j < n {
				if s[j] == c {
					if j+1 < n && s[j+1] == c {
						j += 2
						continue
					}
					break
				}
				j++
			}
			i = j + 1
			b.WriteString(" _ ")
		case c == '$':
			j := i + 1
			for j < n && isWordByte(s[j]) {
				j++
			}
			if j < n && s[j] == '$' {
				tag := s[i : j+1]
				end := strings.Index(s[j+1:], tag)
				if end < 0 {
					return b.String()
				}
				i = j + 1 + end + len(tag)
				b.WriteString(" _ ")
			} else {
				b.WriteByte(c)
				i++
			}
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}

func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) > max {
		return s[:max-3] + "..."
	}
	return s
}
