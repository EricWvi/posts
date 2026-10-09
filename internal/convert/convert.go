// Package convert talks to the wf-posts bridge.
package convert

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"
)

// Article is what the conversion makes of a page.
type Article struct {
	Title string `json:"title"`
	// PublishedDate is YYYY-MM-DD: the date the page gives, or the day of
	// the conversion when it gives none.
	PublishedDate string `json:"published_date"`
	// Slug is lowercase ASCII letters, digits and single dashes.
	Slug     string `json:"slug"`
	Markdown string `json:"markdown"`
}

var (
	slugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	maxSlug     = 120
)

// validate guards the fields that end up in file paths.
func (a Article) validate() error {
	if strings.TrimSpace(a.Title) == "" {
		return errors.New("转换结果缺少标题")
	}
	if t, err := time.Parse(time.DateOnly, a.PublishedDate); err != nil || t.Format(time.DateOnly) != a.PublishedDate {
		return fmt.Errorf("转换结果的发布日期无效: %q", a.PublishedDate)
	}
	if !slugPattern.MatchString(a.Slug) || len(a.Slug) > maxSlug {
		return fmt.Errorf("转换结果的 slug 无效: %q", a.Slug)
	}
	if strings.TrimSpace(a.Markdown) == "" {
		return errors.New("转换结果的 Markdown 为空")
	}
	return nil
}

// Client calls POST /api/convert on the bridge.
type Client struct {
	url  string
	http *http.Client
}

// New returns a client for the bridge at baseURL. Timeout bounds one
// conversion.
func New(baseURL string, timeout time.Duration) *Client {
	return &Client{
		url:  strings.TrimSuffix(baseURL, "/") + "/api/convert",
		http: &http.Client{Timeout: timeout},
	}
}

// Convert sends cleaned html and returns the validated article.
func (c *Client) Convert(ctx context.Context, html []byte) (Article, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(html))
	if err != nil {
		return Article{}, err
	}
	req.Header.Set("Content-Type", "text/html; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return Article{}, fmt.Errorf("无法连接 wf-posts: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return Article{}, fmt.Errorf("读取 wf-posts 响应: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct{ Error string }
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return Article{}, fmt.Errorf("wf-posts %d: %s", resp.StatusCode, e.Error)
		}
		return Article{}, fmt.Errorf("wf-posts %d", resp.StatusCode)
	}
	var a Article
	if err := json.Unmarshal(body, &a); err != nil {
		return Article{}, fmt.Errorf("wf-posts 响应不是合法 JSON: %w", err)
	}
	a.Title = strings.TrimSpace(a.Title)
	return a, a.validate()
}
