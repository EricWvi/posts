package verify

import (
	"encoding/json"
	"os"
	"strings"
	"testing"

	"posts/internal/singlefile"
)

func check(t *testing.T, page, markdown string) Report {
	t.Helper()
	r, err := Check([]byte(page), markdown)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

const page = `<html><head><title>Post | Site</title></head><body>
<nav><a href=/>Home</a></nav>
<article><h1>Hello, world</h1>
<p>Some <em>text</em> with a <a href="https://x.test/a_(b)">link that
wraps</a> and <code>snake_case</code>.</p>
<ol><li>first</li><li>second</li></ol>
<table><tr><th>Name</th><th>Value</th></tr><tr><td>a</td><td>1</td></tr></table>
<pre><span class=ln>1</span>const x = 1;
<span class=ln>2</span>return x &lt; 2;</pre>
<video poster=assets/p.webp>Your browser does not support video.</video>
<img src=assets/i.png alt="An image">
</article></body></html>`

func TestFaithfulMarkdownPasses(t *testing.T) {
	md := "# Hello, world\n\nSome *text* with a [link that\nwraps](https://x.test/a_(b)) and `snake_case`.\n\n" +
		"1. first\n2. second\n\n| Name | Value |\n| --- | --- |\n| a | 1 |\n\n" +
		"```js\nconst x = 1;\nreturn x < 2;\n```\n\n" +
		"<video poster=\"assets/p.webp\">Your browser does not support video.</video>\n\n" +
		"![An image](assets/i.png)\n\n[^ref]: https://x.test\n\n---\n"
	r := check(t, page, md)
	if !r.OK() || r.Checked != 7 {
		t.Fatalf("report = %+v", r)
	}
}

func TestChangedTextIsReported(t *testing.T) {
	md := "# Hello, world\n\nSome text with a link that wraps and snake_case.\n\n" +
		"This sentence was invented.\n\n```\nconst x = 2;\n```\n"
	r := check(t, page, md)
	if r.OK() || r.UnmatchedCount != 2 || r.Unmatched[0] != "This sentence was invented." || r.Unmatched[1] != "const x = 2;" {
		t.Fatalf("report = %+v", r)
	}
}

func TestReportIsBounded(t *testing.T) {
	var md strings.Builder
	for range maxReported + 5 {
		md.WriteString(strings.Repeat("made up ", 100) + "\n\n")
	}
	r := check(t, page, md.String())
	if r.UnmatchedCount != maxReported+5 || len(r.Unmatched) != maxReported {
		t.Fatalf("count %d listed %d", r.UnmatchedCount, len(r.Unmatched))
	}
	if n := len([]rune(r.Unmatched[0])); n != maxExcerpt+1 {
		t.Fatalf("excerpt length %d", n)
	}
}

// The real conversion of the sample page drops the site navigation and
// keeps the article verbatim.
func TestSampleConversion(t *testing.T) {
	src, err := os.ReadFile("../singlefile/testdata/openai-plugin-extensions.html")
	if err != nil {
		t.Fatal(err)
	}
	cleaned, err := singlefile.Clean(src)
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile("testdata/openai-plugin-extensions.json")
	if err != nil {
		t.Fatal(err)
	}
	var article struct{ Markdown string }
	json.Unmarshal(raw, &article)
	r, err := Check(cleaned.HTML, article.Markdown)
	if err != nil {
		t.Fatal(err)
	}
	if !r.OK() || r.Checked < 30 {
		t.Fatalf("report = %+v", r)
	}
	tampered := strings.Replace(article.Markdown, "Declaring support for an extension takes just a few lines",
		"Declaring support for an extension takes only a few lines", 1)
	if r, _ := Check(cleaned.HTML, tampered); r.UnmatchedCount != 1 {
		t.Fatalf("tampered report = %+v", r)
	}
}
