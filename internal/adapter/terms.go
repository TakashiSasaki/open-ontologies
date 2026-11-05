// Copyright (c) 2026 佐々木隆志 (Takashi Sasaki) <takashi316@gmail.com> (https://x.com/TakashiSasaki)
package adapter

import (
	"errors"
	"strings"
	"unicode"
)

type token struct {
	text, kind string
	start, end int
}

func identifier(s string) bool {
	if s == "" {
		return false
	}
	for i, c := range s {
		if !(c == '_' || unicode.IsLetter(c) || (i > 0 && unicode.IsDigit(c))) {
			return false
		}
	}
	return true
}

// A lexical boundary check, not a replacement for the upstream SPARQL parser.
// Literals, comments, full IRIREFs and whole-term placeholders stay indivisible.
func tokens(s string) ([]token, error) {
	var out []token
	for i := 0; i < len(s); {
		if strings.ContainsRune(" \t\r\n", rune(s[i])) {
			i++
			continue
		}
		start := i
		kind := "word"
		switch s[i] {
		case '#':
			for i < len(s) && s[i] != '\n' {
				i++
			}
			kind = "comment"
		case '\'', '"':
			quote := s[i : i+1]
			if strings.HasPrefix(s[i:], strings.Repeat(quote, 3)) {
				quote = strings.Repeat(quote, 3)
			}
			i += len(quote)
			closed := false
			for i < len(s) {
				if s[i] == '\\' {
					i += 2
					continue
				}
				if strings.HasPrefix(s[i:], quote) {
					i += len(quote)
					closed = true
					break
				}
				i++
			}
			if !closed {
				return nil, errors.New("unterminated SPARQL literal")
			}
			kind = "literal"
		case '<':
			if end := strings.IndexByte(s[i:], '>'); end >= 0 && !strings.ContainsAny(s[i+1:i+end], " \t\r\n") {
				i += end + 1
				kind = "iri"
			} else {
				i++
				kind = "punct"
			}
		case '{':
			if end := strings.IndexByte(s[i:], '}'); end > 1 && identifier(s[i+1:i+end]) {
				i += end + 1
				kind = "placeholder"
			} else {
				i++
				kind = "punct"
			}
		case '}', '(', ')', '[', ']', ';', ',', '.':
			i++
			kind = "punct"
		default:
			for i < len(s) && !strings.ContainsRune(" \t\r\n{}()[];,<>\"'#", rune(s[i])) {
				i++
			}
			if i == start {
				i++
				kind = "punct"
			}
		}
		out = append(out, token{s[start:i], kind, start, i})
	}
	return out, nil
}
func placeholder(t token) string {
	if t.kind == "placeholder" {
		return t.text[1 : len(t.text)-1]
	}
	if t.kind == "iri" && strings.HasPrefix(t.text, "<{") && strings.HasSuffix(t.text, "}>") && identifier(t.text[2:len(t.text)-2]) {
		return t.text[2 : len(t.text)-2]
	}
	return ""
}

// Values are serialized RDF IRI terms, never SPARQL fragments. Partial IRI or
// literal templates are rejected: replacing those would change their meaning.
func bindTerms(s string, bindings map[string]string) (string, error) {
	ts, e := tokens(s)
	if e != nil {
		return "", e
	}
	var b strings.Builder
	pos := 0
	for _, t := range ts {
		name := placeholder(t)
		if name == "" {
			if strings.Contains(t.text, "{") && t.kind != "punct" {
				return "", errors.New("only whole RDF-term placeholders are supported; partial IRI/literal templates must be migrated")
			}
			continue
		}
		if t.start > 0 && !strings.ContainsRune(" \t\r\n({[;,", rune(s[t.start-1])) {
			return "", errors.New("placeholder is not a complete RDF term")
		}
		if t.end < len(s) && !strings.ContainsRune(" \t\r\n)}];,.", rune(s[t.end])) {
			return "", errors.New("placeholder is not a complete RDF term")
		}
		value, ok := bindings[name]
		if !ok {
			return "", errors.New("missing binding for placeholder " + name)
		}
		b.WriteString(s[pos:t.start])
		b.WriteString(value)
		pos = t.end
	}
	b.WriteString(s[pos:])
	return b.String(), nil
}

func significant(s string) ([]token, error) {
	ts, e := tokens(s)
	var out []token
	for _, t := range ts {
		if t.kind != "comment" {
			out = append(out, t)
		}
	}
	return out, e
}
func queryKind(s string) string {
	ts, e := significant(s)
	if e != nil {
		return ""
	}
	for i := 0; i < len(ts); {
		switch strings.ToUpper(ts[i].text) {
		case "PREFIX":
			i += 3
		case "BASE":
			i += 2
		default:
			return strings.ToUpper(ts[i].text)
		}
	}
	return ""
}

func simpleAsk(s string) ([]token, bool) {
	ts, e := significant(s)
	if e != nil {
		return nil, false
	}
	if len(ts) == 7 && ts[5].text == "." {
		ts = append(ts[:5], ts[6])
	}
	return ts, e == nil && len(ts) == 6 && strings.EqualFold(ts[0].text, "ASK") && ts[1].text == "{" && ts[5].text == "}"
}
func normalizePrecondition(s string, parameters map[string]string) (string, error) {
	q, e := bindTerms(s, parameters)
	if e != nil {
		return "", e
	}
	if ts, ok := simpleAsk(q); ok && ts[3].text == "a" {
		q = q[:ts[3].start] + "<" + rdf + "type>" + q[ts[3].end:]
	}
	return q, nil
}
