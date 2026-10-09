// Package verify checks that converted Markdown only contains text that is
// on the page it was converted from.
//
// The conversion keeps the article and drops everything around it, so the
// page legitimately has far more text than the Markdown. The reverse is
// what matters: text the model rewrote, translated or made up is not on the
// page. Check compares letters and digits only, so formatting, whitespace
// and punctuation differences never count.
package verify

import (
	"bytes"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode"

	nethtml "golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

const (
	// maxReported caps how many unmatched blocks a report lists.
	maxReported = 20
	// maxExcerpt caps the length of one listed block, in runes.
	maxExcerpt = 300
)

// Report is the outcome of a check.
type Report struct {
	// Checked is the number of Markdown blocks that carry text.
	Checked int `json:"checked"`
	// UnmatchedCount is the number of blocks not found on the page.
	UnmatchedCount int `json:"unmatchedCount"`
	// Unmatched lists the first of those blocks, shortened.
	Unmatched []string `json:"unmatched"`
}

// OK reports whether every block was found on the page.
func (r Report) OK() bool { return r.UnmatchedCount == 0 }

// Check looks for every text block of markdown in the page html.
//
// Prose is checked a blank-line separated block at a time, so a link or
// emphasis spanning a line break stays intact. Code is checked line by
// line, because pages often interleave line numbers with the code.
func Check(page []byte, markdown string) (Report, error) {
	text, err := pageText(page)
	if err != nil {
		return Report{}, err
	}
	report := Report{Unmatched: []string{}}
	for _, block := range blocks(markdown) {
		key := normalize(block.text)
		if len([]rune(key)) < 2 {
			continue
		}
		report.Checked++
		if strings.Contains(text, key) {
			continue
		}
		report.UnmatchedCount++
		if len(report.Unmatched) < maxReported {
			report.Unmatched = append(report.Unmatched, excerpt(block.raw))
		}
	}
	return report, nil
}

// pageText is the normalized text of every text node outside scripts and
// styles, in document order.
func pageText(page []byte) (string, error) {
	doc, err := nethtml.Parse(bytes.NewReader(page))
	if err != nil {
		return "", fmt.Errorf("parse html: %w", err)
	}
	var b strings.Builder
	var walk func(*nethtml.Node)
	walk = func(n *nethtml.Node) {
		if n.Type == nethtml.ElementNode && (n.DataAtom == atom.Script || n.DataAtom == atom.Style) {
			return
		}
		if n.Type == nethtml.TextNode {
			b.WriteString(n.Data)
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(doc)
	return normalize(b.String()), nil
}

type block struct {
	raw  string // as written in the Markdown
	text string // with Markdown syntax removed
}

var (
	fence      = regexp.MustCompile("^\\s*(```+|~~~+)")
	refDef     = regexp.MustCompile(`^\s{0,3}\[[^\]]+\]:\s`)
	image      = regexp.MustCompile(`!\[[^\]]*\]\((?:[^()]|\([^()]*\))*\)`)
	link       = regexp.MustCompile(`\[([^\]]*)\]\((?:[^()]|\([^()]*\))*\)`)
	tag        = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	lineMarker = regexp.MustCompile(`(?m)^\s*(?:>\s*)*(?:#{1,6}\s+|(?:[-*+]|\d+[.)])\s+(?:\[[ xX]\]\s+)?)?`)
)

// blocks splits markdown into prose blocks and code lines.
func blocks(markdown string) []block {
	var out []block
	var para []string
	flush := func() {
		if len(para) == 0 {
			return
		}
		raw := strings.Join(para, "\n")
		out = append(out, block{raw: raw, text: prose(raw)})
		para = nil
	}
	var inCode string // the opening fence while inside a code block
	for line := range strings.Lines(markdown) {
		line = strings.TrimRight(line, "\r\n")
		if m := fence.FindStringSubmatch(line); m != nil {
			switch {
			case inCode == "":
				flush()
				inCode = m[1]
				continue
			case strings.HasPrefix(m[1], inCode):
				inCode = ""
				continue
			}
		}
		switch {
		case inCode != "":
			out = append(out, block{raw: line, text: line})
		case strings.TrimSpace(line) == "":
			flush()
		case refDef.MatchString(line):
		default:
			para = append(para, line)
		}
	}
	flush()
	return out
}

// prose removes Markdown syntax that is not text on the page: images,
// link targets, html tags and block markers.
func prose(s string) string {
	s = image.ReplaceAllString(s, "")
	s = link.ReplaceAllString(s, "$1")
	s = tag.ReplaceAllString(s, "")
	s = lineMarker.ReplaceAllString(s, "")
	return html.UnescapeString(s)
}

// normalize keeps only lowercased letters and digits.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

func excerpt(s string) string {
	r := []rune(s)
	if len(r) <= maxExcerpt {
		return s
	}
	return string(r[:maxExcerpt]) + "…"
}
