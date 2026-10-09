package singlefile

import (
	"bytes"
	"encoding/base64"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"
)

func mustClean(t *testing.T, src string) Page {
	t.Helper()
	p, err := Clean([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	return p
}

func b64(data string) string { return base64.StdEncoding.EncodeToString([]byte(data)) }

func TestHeader(t *testing.T) {
	p := mustClean(t, `<!DOCTYPE html><html><!--
 Page saved with SingleFile 
 url: https://example.com/post 
 saved date: Thu Oct 08 2026 10:21:57 GMT+0800 (中国标准时间)
--><head><title> Hello  </title></head><body><p>x</p></body></html>`)
	if p.Title != "Hello" || p.SourceURL != "https://example.com/post" {
		t.Fatalf("title %q url %q", p.Title, p.SourceURL)
	}
	want := time.Date(2026, 10, 8, 2, 21, 57, 0, time.UTC)
	if !p.SavedAt.Equal(want) {
		t.Fatalf("saved %v, want %v", p.SavedAt, want)
	}
	if bytes.Contains(p.HTML, []byte("SingleFile")) {
		t.Fatal("comment kept")
	}
}

func TestRemovesStylingAndScripting(t *testing.T) {
	font := "url(data:font/woff2;base64," + b64("font") + ")"
	p := mustClean(t, `<html><head>
<meta charset=utf-8><meta name=viewport content=x><meta http-equiv=refresh content=0>
<meta name=description content=desc><meta property=article:published_time content=2026-01-02>
<style>@font-face{src:`+font+`}</style><link rel=stylesheet href=a.css><link rel=icon href="data:image/png;base64,`+b64("icon")+`">
<script>alert(1)</script><script type=application/ld+json>{"datePublished":"2026-01-02"}</script>
</head><body onload=x() style="color:red">
<noscript>enable js</noscript>
<div class="post body" data-foo=1 data-src=y style="font-family:x" onclick=go()>
<p>Text <svg viewBox="0 0 1 1"><path d=M0/></svg>kept</p>
<ul class="sf-hidden"><li>hidden nav</li></ul>
<iframe src=https://example.com/embed srcdoc="<p>x</p>"></iframe>
</div></body></html>`)
	out := string(p.HTML)
	for _, gone := range []string{"<style", "<script>", "alert", "<link", "<noscript", "<svg", "<path",
		"style=", "onclick", "onload", "data-foo", "data-src", "viewport", "refresh", "srcdoc", "hidden nav", "font"} {
		if strings.Contains(out, gone) {
			t.Errorf("output still contains %q:\n%s", gone, out)
		}
	}
	for _, kept := range []string{`class="post body"`, "Text kept", `<meta charset="utf-8"/>`,
		`name="description"`, `property="article:published_time"`, `"datePublished":"2026-01-02"`,
		`<iframe src="https://example.com/embed">`} {
		if !strings.Contains(out, kept) {
			t.Errorf("output lost %q:\n%s", kept, out)
		}
	}
	if len(p.Assets) != 0 {
		t.Fatalf("assets only styles or links used were kept: %+v", p.Assets)
	}
}

func TestUnwrapsShadowRoots(t *testing.T) {
	p := mustClean(t, `<body><my-card><template shadowrootmode=open><style>:host{}</style><p>shadow text</p></template></my-card></body>`)
	out := string(p.HTML)
	if strings.Contains(out, "template") || strings.Contains(out, ":host") || !strings.Contains(out, "<my-card><p>shadow text</p></my-card>") {
		t.Fatalf("shadow root not unwrapped:\n%s", out)
	}
}

func TestExtractsMedia(t *testing.T) {
	png := "data:image/png;base64," + b64("png bytes")
	webp := "data:image/webp;base64," + b64("webp bytes")
	svg := "data:image/svg+xml,%3Csvg%3E%3C%2Fsvg%3E"
	mp4 := "data:video/mp4;base64," + b64("mp4 bytes")
	p := mustClean(t, `<body>
<img src="`+png+`" alt=a>
<img src="`+png+`" srcset="`+png+` 1x, `+webp+` 2x">
<picture><source srcset="`+webp+`, `+png+`"><img src="`+svg+`"></picture>
<video poster="`+webp+`"><source src="`+mp4+`" type=video/mp4></video>
<a href="`+png+`">download</a>
<img src="https://example.com/remote.png">
</body>`)
	names := map[string]string{}
	for _, a := range p.Assets {
		names[a.MediaType] = a.Name
	}
	if len(p.Assets) != 4 || len(names) != 4 {
		t.Fatalf("assets = %+v", p.Assets)
	}
	for mediaType, data := range map[string]string{"image/png": "png bytes", "image/webp": "webp bytes",
		"image/svg+xml": "<svg></svg>", "video/mp4": "mp4 bytes"} {
		for _, a := range p.Assets {
			if a.MediaType == mediaType && string(a.Data) != data {
				t.Errorf("%s data = %q", mediaType, a.Data)
			}
		}
	}
	if !regexp.MustCompile(`^[0-9a-f]{16}\.png$`).MatchString(names["image/png"]) ||
		!strings.HasSuffix(names["image/svg+xml"], ".svg") || !strings.HasSuffix(names["video/mp4"], ".mp4") {
		t.Fatalf("names = %v", names)
	}
	out := string(p.HTML)
	ref := func(mediaType string) string { return "assets/" + names[mediaType] }
	for _, want := range []string{
		`<img src="` + ref("image/png") + `" alt="a"/>`,
		`srcset="` + ref("image/png") + ` 1x, ` + ref("image/webp") + ` 2x"`,
		`<source srcset="` + ref("image/webp") + `, ` + ref("image/png") + `"/>`,
		`<img src="` + ref("image/svg+xml") + `"/>`,
		`<video poster="` + ref("image/webp") + `"><source src="` + ref("video/mp4") + `" type="video/mp4"/></video>`,
		`<a href="` + ref("image/png") + `">download</a>`,
		`<img src="https://example.com/remote.png"/>`,
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %s in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "data:") {
		t.Errorf("data: URI left in:\n%s", out)
	}
}

func TestMalformedDataURIIsKept(t *testing.T) {
	p := mustClean(t, `<img src="data:image/png;base64,!!!">`)
	if len(p.Assets) != 0 || !strings.Contains(string(p.HTML), `src="data:image/png;base64,!!!"`) {
		t.Fatalf("assets %+v html %s", p.Assets, p.HTML)
	}
}

func TestSample(t *testing.T) {
	src, err := os.ReadFile("testdata/openai-plugin-extensions.html")
	if err != nil {
		t.Fatal(err)
	}
	p, err := Clean(src)
	if err != nil {
		t.Fatal(err)
	}
	if p.Title != "Plugin Extensions – Plugins | OpenAI Developers" ||
		p.SourceURL != "https://developers.openai.com/plugins/build/extensions" || p.SavedAt.IsZero() {
		t.Fatalf("meta: %q %q %v", p.Title, p.SourceURL, p.SavedAt)
	}
	if len(p.HTML) > len(src)/10 {
		t.Errorf("cleaned html is %d of %d bytes", len(p.HTML), len(src))
	}
	out := string(p.HTML)
	for _, gone := range []string{"<style", "<script", "<svg", "data:", "style=", "data-", "sf-hidden", "@font-face"} {
		if strings.Contains(out, gone) {
			t.Errorf("output still contains %q", gone)
		}
	}
	for _, kept := range []string{"Plugin Extensions", "OpenAI MCP Extensions enables developers", `class="`, "<video", `poster="assets/`} {
		if !strings.Contains(out, kept) {
			t.Errorf("output lost %q", kept)
		}
	}
	kinds := map[string]bool{}
	for _, a := range p.Assets {
		kinds[a.MediaType] = true
		if !strings.Contains(out, "assets/"+a.Name) {
			t.Errorf("asset %s not referenced", a.Name)
		}
	}
	for _, k := range []string{"image/png", "image/webp", "image/svg+xml"} {
		if !kinds[k] {
			t.Errorf("no %s asset", k)
		}
	}
	for k := range kinds {
		if strings.HasPrefix(k, "font/") {
			t.Errorf("font asset %s kept", k)
		}
	}
}
