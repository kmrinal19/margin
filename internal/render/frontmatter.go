package render

import (
	"bytes"
	"strings"
)

// frontMatter holds the optional YAML-ish header of a document. Only a small,
// fixed set of scalar keys is supported, so we parse it directly and avoid a
// YAML dependency (keeping margin's tree tiny).
type frontMatter struct {
	Title  string
	Date   string
	Status string
	Lead   string
}

var fmDelim = []byte("---")

// splitFrontMatter separates a leading `--- … ---` block from the Markdown body.
// If there is no front matter, it returns the zero frontMatter and the input
// unchanged.
func splitFrontMatter(src []byte) (frontMatter, []byte) {
	// Must start with a "---" line.
	rest := src
	first, after, ok := nextLine(rest)
	if !ok || !bytes.Equal(bytes.TrimRight(first, " \t\r"), fmDelim) {
		return frontMatter{}, src
	}
	var fields []string
	rest = after
	for {
		line, next, ok := nextLine(rest)
		if !ok {
			// Unterminated front matter — treat the whole input as body.
			return frontMatter{}, src
		}
		if bytes.Equal(bytes.TrimRight(line, " \t\r"), fmDelim) {
			rest = next
			break
		}
		fields = append(fields, string(line))
		rest = next
	}
	return parseFrontMatter(fields), rest
}

func nextLine(b []byte) (line, remainder []byte, ok bool) {
	if len(b) == 0 {
		return nil, nil, false
	}
	if i := bytes.IndexByte(b, '\n'); i >= 0 {
		return b[:i], b[i+1:], true
	}
	return b, nil, true
}

func parseFrontMatter(lines []string) frontMatter {
	var fm frontMatter
	for _, ln := range lines {
		i := strings.IndexByte(ln, ':')
		if i < 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(ln[:i]))
		val := unquote(strings.TrimSpace(ln[i+1:]))
		switch key {
		case "title":
			fm.Title = val
		case "date":
			fm.Date = val
		case "status":
			fm.Status = val
		case "lead", "description", "summary":
			fm.Lead = val
		}
	}
	return fm
}

func unquote(s string) string {
	if len(s) >= 2 {
		if (s[0] == '"' && s[len(s)-1] == '"') || (s[0] == '\'' && s[len(s)-1] == '\'') {
			return s[1 : len(s)-1]
		}
	}
	return s
}
