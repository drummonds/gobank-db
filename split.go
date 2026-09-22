package db

import "strings"

// SplitStatements splits a SQL script into individual statements on
// top-level semicolons. It understands single- and double-quoted strings,
// dollar-quoted blocks ($$ ... $$ and $tag$ ... $tag$), line and block
// comments, and BEGIN ... END / CASE ... END bodies (so trigger bodies with
// embedded semicolons stay intact). Empty statements are dropped and
// surrounding whitespace is trimmed.
func SplitStatements(script string) []string {
	var (
		out   []string
		cur   strings.Builder
		depth int // BEGIN/CASE ... END nesting
		i     int
	)
	src := script
	n := len(src)

	flush := func() {
		s := strings.TrimSpace(cur.String())
		cur.Reset()
		if s != "" {
			out = append(out, s)
		}
	}

	// readWord returns the identifier/keyword starting at i, or "".
	readWord := func(i int) string {
		j := i
		for j < n && isWordByte(src[j]) {
			j++
		}
		return src[i:j]
	}

	for i < n {
		c := src[i]
		switch {
		case c == '-' && i+1 < n && src[i+1] == '-':
			// Line comment: keep it (harmless), stop at newline.
			j := strings.IndexByte(src[i:], '\n')
			if j < 0 {
				j = n - i
			}
			cur.WriteString(src[i : i+j])
			i += j

		case c == '/' && i+1 < n && src[i+1] == '*':
			j := strings.Index(src[i+2:], "*/")
			if j < 0 {
				j = n - i - 2
			}
			cur.WriteString(src[i : i+2+j+2])
			i += 2 + j + 2
			if i > n {
				i = n
			}

		case c == '\'' || c == '"':
			// Quoted string/identifier; doubled quote is an escape.
			j := i + 1
			for j < n {
				if src[j] == c {
					if j+1 < n && src[j+1] == c {
						j += 2
						continue
					}
					break
				}
				j++
			}
			if j < n {
				j++ // closing quote
			}
			cur.WriteString(src[i:j])
			i = j

		case c == '$':
			// Dollar quoting: $$ or $tag$.
			j := i + 1
			for j < n && isWordByte(src[j]) {
				j++
			}
			if j < n && src[j] == '$' {
				tag := src[i : j+1]
				end := strings.Index(src[j+1:], tag)
				if end < 0 {
					cur.WriteString(src[i:])
					i = n
				} else {
					stop := j + 1 + end + len(tag)
					cur.WriteString(src[i:stop])
					i = stop
				}
			} else {
				cur.WriteByte(c)
				i++
			}

		case isWordByte(c) && (i == 0 || !isWordByte(src[i-1])):
			w := readWord(i)
			switch strings.ToUpper(w) {
			case "BEGIN":
				// BEGIN [TRANSACTION|WORK|;] starts a transaction, not a block.
				if !beginsTransaction(src[i+len(w):]) {
					depth++
				}
			case "CASE":
				depth++
			case "END":
				if depth > 0 {
					depth--
				}
			}
			cur.WriteString(w)
			i += len(w)

		case c == ';' && depth == 0:
			flush()
			i++

		default:
			cur.WriteByte(c)
			i++
		}
	}
	flush()
	return out
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}

// beginsTransaction reports whether the text after a BEGIN keyword makes it
// a transaction statement (BEGIN; / BEGIN TRANSACTION / BEGIN WORK /
// BEGIN ISOLATION ...) rather than the opening of a block body.
func beginsTransaction(rest string) bool {
	rest = strings.TrimLeft(rest, " \t\r\n")
	if rest == "" || rest[0] == ';' {
		return true
	}
	j := 0
	for j < len(rest) && isWordByte(rest[j]) {
		j++
	}
	switch strings.ToUpper(rest[:j]) {
	case "TRANSACTION", "WORK", "ISOLATION", "READ", "DEFERRABLE", "NOT":
		return true
	}
	return false
}
