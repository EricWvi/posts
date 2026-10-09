// Package singlefile cleans a page saved by the SingleFile browser
// extension so that only its content and structure remain.
//
// SingleFile inlines everything a page needs into one html file: styles,
// scripts, fonts and images as data: URIs. Clean drops the styling and
// scripting and turns the inlined media into separate asset files that the
// html references by relative path. It neither reads nor writes the disk.
package singlefile

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// AssetDir is the directory, relative to the html file, that assets are
// referenced from.
const AssetDir = "assets"

// Page is a cleaned page.
type Page struct {
	// HTML is the cleaned document. Media references point into AssetDir.
	HTML []byte
	// Assets are the media files the html references, each listed once.
	Assets []Asset
	// Title is the text of the <title> element.
	Title string
	// SourceURL is the address the page was saved from, when SingleFile
	// recorded it.
	SourceURL string
	// SavedAt is when the page was saved, or the zero time if unknown.
	SavedAt time.Time
}

// Asset is a media file extracted from a data: URI.
type Asset struct {
	// Name is the file name inside AssetDir: a content hash plus an
	// extension that matches the media type.
	Name      string
	MediaType string
	Data      []byte
}

// Clean parses a SingleFile page and returns its cleaned form.
//
// It removes:
//   - <style>, <script> (except JSON-LD metadata), <noscript>, <link>,
//     <base>, <template> wrappers of declarative shadow roots (their
//     content is kept) and inline <svg>;
//   - elements SingleFile marked as hidden (class "sf-hidden"), which were
//     not displayed when the page was saved;
//   - page chrome: header, nav, footer and aside elements (or their ARIA
//     roles) outside any article or main element, unless they hold an h1;
//   - style, on* event handler, data-* and srcdoc attributes;
//   - utility classes such as Tailwind's "px-3" or "lg:flex", which only
//     style the page. Classes that name a part, such as "reply_content",
//     stay because they help tell the article from the rest of the page;
//   - meta elements other than charset and named metadata;
//   - comments.
//
// Resources only styles referred to, such as fonts, go away with the styles.
// Every remaining data: URI in an attribute becomes an asset.
func Clean(src []byte) (Page, error) {
	doc, err := html.Parse(bytes.NewReader(src))
	if err != nil {
		return Page{}, fmt.Errorf("parse html: %w", err)
	}
	c := &cleaner{assets: map[string]struct{}{}}
	c.readHeader(doc)
	c.clean(doc, false)

	var out bytes.Buffer
	if err := html.Render(&out, doc); err != nil {
		return Page{}, fmt.Errorf("render html: %w", err)
	}
	return Page{
		HTML:      out.Bytes(),
		Assets:    c.list,
		Title:     strings.TrimSpace(c.title),
		SourceURL: c.sourceURL,
		SavedAt:   c.savedAt,
	}, nil
}

type cleaner struct {
	assets    map[string]struct{} // names already in list
	list      []Asset
	title     string
	sourceURL string
	savedAt   time.Time
}

// readHeader picks the source url and save date out of the comment
// SingleFile puts at the top of the document:
//
//	Page saved with SingleFile
//	url: https://example.com/post
//	saved date: Thu Oct 08 2026 10:21:57 GMT+0800 (中国标准时间)
func (c *cleaner) readHeader(n *html.Node) {
	for n := range n.Descendants() {
		if n.Type != html.CommentNode || !strings.Contains(n.Data, "SingleFile") {
			continue
		}
		for line := range strings.Lines(n.Data) {
			key, value, ok := strings.Cut(strings.TrimSpace(line), ":")
			if !ok {
				continue
			}
			value = strings.TrimSpace(value)
			switch key {
			case "url":
				c.sourceURL = value
			case "saved date":
				c.savedAt = parseSavedDate(value)
			}
		}
		return
	}
}

// parseSavedDate reads JavaScript's Date.prototype.toString format.
func parseSavedDate(s string) time.Time {
	if i := strings.Index(s, " ("); i >= 0 {
		s = s[:i]
	}
	t, err := time.Parse("Mon Jan 02 2006 15:04:05 GMT-0700", s)
	if err != nil {
		return time.Time{}
	}
	return t
}

// clean cleans the children of n. inContent reports whether n is inside an
// article or main element, where headers, navs and asides belong to the
// article.
func (c *cleaner) clean(n *html.Node, inContent bool) {
	for child := n.FirstChild; child != nil; {
		next := child.NextSibling
		switch {
		case child.Type == html.CommentNode:
			n.RemoveChild(child)
		case child.Type == html.ElementNode && (c.drop(child) || !inContent && isChrome(child)):
			n.RemoveChild(child)
		case child.Type == html.ElementNode && child.DataAtom == atom.Template && hasAttr(child, "shadowrootmode"):
			// Declarative shadow DOM: the template's content is what the
			// host displayed, so it takes the template's place.
			c.clean(child, inContent)
			for grand := child.FirstChild; grand != nil; {
				after := grand.NextSibling
				child.RemoveChild(grand)
				n.InsertBefore(grand, child)
				grand = after
			}
			n.RemoveChild(child)
		default:
			if child.Type == html.ElementNode {
				if child.DataAtom == atom.Title && c.title == "" {
					c.title = text(child)
				}
				c.cleanAttrs(child)
			}
			c.clean(child, inContent || isContent(child))
		}
		child = next
	}
}

// drop reports whether an element is removed together with its content.
func (c *cleaner) drop(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Style, atom.Noscript, atom.Link, atom.Base, atom.Svg:
		return true
	case atom.Script:
		return !strings.EqualFold(attr(n, "type"), "application/ld+json")
	case atom.Meta:
		if hasAttr(n, "charset") {
			return false
		}
		name := strings.ToLower(attr(n, "name"))
		if name == "viewport" || name == "referrer" || name == "theme-color" {
			return true
		}
		return name == "" && attr(n, "property") == "" && attr(n, "itemprop") == ""
	}
	for _, class := range strings.Fields(attr(n, "class")) {
		if class == "sf-hidden" {
			return true
		}
	}
	return false
}

// isChrome reports whether an element is part of the page around the
// article. A header holding the h1 is kept: some themes put the article
// title in a header next to, rather than inside, the article.
func isChrome(n *html.Node) bool {
	switch n.DataAtom {
	case atom.Header, atom.Nav, atom.Footer, atom.Aside:
	default:
		switch attr(n, "role") {
		case "banner", "navigation", "contentinfo", "complementary":
		default:
			return false
		}
	}
	for d := range n.Descendants() {
		if d.Type == html.ElementNode && d.DataAtom == atom.H1 {
			return false
		}
	}
	return true
}

// isContent reports whether an element holds the article.
func isContent(n *html.Node) bool {
	if n.Type != html.ElementNode {
		return false
	}
	if n.DataAtom == atom.Article || n.DataAtom == atom.Main {
		return true
	}
	role := attr(n, "role")
	return role == "main" || role == "article"
}

func (c *cleaner) cleanAttrs(n *html.Node) {
	kept := n.Attr[:0]
	for _, a := range n.Attr {
		key := strings.ToLower(a.Key)
		if a.Namespace != "" || key == "style" || key == "srcdoc" ||
			strings.HasPrefix(key, "on") || strings.HasPrefix(key, "data-") {
			continue
		}
		switch {
		case key == "class":
			if a.Val = semanticClasses(a.Val); a.Val == "" {
				continue
			}
		case key == "srcset" || key == "imagesrcset":
			a.Val = dataURIs.ReplaceAllStringFunc(a.Val, c.extract)
		case hasDataScheme(a.Val):
			a.Val = c.extract(strings.TrimSpace(a.Val))
		}
		kept = append(kept, a)
	}
	n.Attr = kept
}

// semanticClasses drops the utility classes from a class attribute.
func semanticClasses(v string) string {
	var kept []string
	for _, class := range strings.Fields(v) {
		if !isUtilityClass(class) {
			kept = append(kept, class)
		}
	}
	return strings.Join(kept, " ")
}

// Utility classes in the style of Tailwind: a property prefix and a value,
// such as "px-3", "text-sm" or "line-clamp-2", or one of a few keywords.
var (
	utilityPrefix = regexp.MustCompile(`^(?:[pm][xytrblse]?|[wh]|size|min-[wh]|max-[wh]|gap(?:-[xy])?|space-[xy]|` +
		`text|font|leading|tracking|bg|border|rounded|ring|shadow|outline|divide|opacity|z|` +
		`top|bottom|left|right|inset(?:-[xy])?|flex|grid|grid-cols|grid-rows|col|row|order|` +
		`items|justify|self|place|content|overflow|whitespace|line-clamp|object|aspect|` +
		`duration|delay|ease|transition|translate-[xy]|scale|rotate|select|shrink|grow|basis|` +
		`underline-offset|decoration|cursor|pointer-events|list|fill|stroke)-[a-z0-9.]+$`)
	utilityKeywords = map[string]bool{
		"flex": true, "grid": true, "block": true, "inline": true, "inline-block": true, "inline-flex": true,
		"hidden": true, "contents": true, "relative": true, "absolute": true, "fixed": true, "sticky": true,
		"visible": true, "invisible": true, "truncate": true, "underline": true, "no-underline": true,
		"italic": true, "uppercase": true, "lowercase": true, "capitalize": true, "border": true,
		"rounded": true, "shadow": true, "transition": true, "sr-only": true, "group": true, "peer": true,
		"grow": true, "shrink": true, "not-prose": true,
	}
)

// isUtilityClass reports whether a class only styles the element. Variants
// ("lg:flex"), arbitrary values ("w-[218px]"), opacity ("bg-black/50") and
// negative values ("-top-0.5") give utilities away by their punctuation.
func isUtilityClass(class string) bool {
	if strings.ContainsAny(class, ":[/") || strings.HasPrefix(class, "-") {
		return true
	}
	return utilityKeywords[class] || utilityPrefix.MatchString(class)
}

// dataURIs matches the data: URIs inside a srcset. Base64 payloads contain
// commas but never whitespace, which separates a URI from its descriptor.
var dataURIs = regexp.MustCompile(`(?i)data:[^\s]+`)

func hasDataScheme(v string) bool {
	v = strings.TrimSpace(v)
	return len(v) > 5 && strings.EqualFold(v[:5], "data:")
}

// extract stores a data: URI as an asset and returns its relative path. A
// URI that cannot be decoded is left as it is.
func (c *cleaner) extract(uri string) string {
	// In a srcset without descriptors the separating comma sticks to the
	// match; a payload never ends with one.
	if trimmed, ok := strings.CutSuffix(uri, ","); ok {
		return c.extract(trimmed) + ","
	}
	mediaType, data, err := decodeDataURI(uri)
	if err != nil {
		return uri
	}
	sum := sha256.Sum256(data)
	name := hex.EncodeToString(sum[:8]) + extension(mediaType)
	if _, ok := c.assets[name]; !ok {
		c.assets[name] = struct{}{}
		c.list = append(c.list, Asset{Name: name, MediaType: mediaType, Data: data})
	}
	return AssetDir + "/" + name
}

// decodeDataURI decodes data:[<mediatype>][;base64],<data>.
func decodeDataURI(uri string) (string, []byte, error) {
	header, payload, ok := strings.Cut(uri[len("data:"):], ",")
	if !ok {
		return "", nil, errors.New("data uri without comma")
	}
	params := strings.Split(header, ";")
	isBase64 := false
	if last := params[len(params)-1]; strings.EqualFold(last, "base64") {
		isBase64 = true
		params = params[:len(params)-1]
	}
	mediaType := strings.ToLower(strings.TrimSpace(params[0]))
	if mediaType == "" {
		mediaType = "text/plain"
	}
	if isBase64 {
		data, err := base64.StdEncoding.DecodeString(payload)
		if err != nil {
			data, err = base64.RawStdEncoding.DecodeString(strings.TrimRight(payload, "="))
		}
		return mediaType, data, err
	}
	data, err := url.PathUnescape(payload)
	return mediaType, []byte(data), err
}

var extensions = map[string]string{
	"image/png":       ".png",
	"image/jpeg":      ".jpg",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"image/avif":      ".avif",
	"image/svg+xml":   ".svg",
	"image/x-icon":    ".ico",
	"image/bmp":       ".bmp",
	"video/mp4":       ".mp4",
	"video/webm":      ".webm",
	"audio/mpeg":      ".mp3",
	"audio/ogg":       ".ogg",
	"audio/wav":       ".wav",
	"text/plain":      ".txt",
	"application/pdf": ".pdf",
}

func extension(mediaType string) string {
	if ext, ok := extensions[mediaType]; ok {
		return ext
	}
	if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ".bin"
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

func text(n *html.Node) string {
	var b strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
		}
	}
	return b.String()
}
