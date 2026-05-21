package db

import (
	"regexp"
	"strings"
	"unicode"
)

var searchSeparatorRunes = map[rune]struct{}{
	'!':  {},
	'"':  {},
	'#':  {},
	'$':  {},
	'%':  {},
	'&':  {},
	'\'': {},
	'(':  {},
	')':  {},
	'*':  {},
	'+':  {},
	',':  {},
	'-':  {},
	'.':  {},
	'/':  {},
	':':  {},
	';':  {},
	'<':  {},
	'=':  {},
	'>':  {},
	'?':  {},
	'@':  {},
	'[':  {},
	'\\': {},
	']':  {},
	'^':  {},
	'_':  {},
	'`':  {},
	'{':  {},
	'|':  {},
	'}':  {},
	'~':  {},
}

func cleanSearchQuery(q string) string {
	var b strings.Builder
	b.Grow(len(q))

	for _, r := range strings.ToLower(q) {
		if _, ok := searchSeparatorRunes[r]; ok || unicode.IsSpace(r) {
			b.WriteRune(' ')
			continue
		}
		b.WriteRune(r)
	}

	return b.String()
}

func extractSearchTerms(q string) []string {
	seen := make(map[string]struct{})
	terms := make([]string, 0)

	for _, word := range strings.Fields(cleanSearchQuery(q)) {
		if len(word) < 2 || isSearchStopWord(word) {
			continue
		}
		if _, ok := seen[word]; ok {
			continue
		}

		seen[word] = struct{}{}
		terms = append(terms, word)
	}

	return terms
}

func wholeWordSearchPattern(term string) string {
	return `\m` + regexp.QuoteMeta(term) + `\M`
}
