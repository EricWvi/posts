package store

import (
	"net/url"
	"strings"
	"time"
)

// ArticleMetadata is the user-confirmed article.json sent to Palace.
type ArticleMetadata struct {
	Title  string `json:"title"`
	URL    string `json:"url"`
	Date   string `json:"date"`
	Author string `json:"author,omitempty"`
}

// ImportMetadata uses the last confirmed values, falling back to conversion metadata.
func (p Post) ImportMetadata() ArticleMetadata {
	if p.PalaceMetadata != nil {
		return *p.PalaceMetadata
	}
	return ArticleMetadata{Title: p.Title, URL: p.SourceURL, Date: p.PublishedDate, Author: p.Author}
}

// Validate rejects incomplete input before it becomes a background import.
func (m ArticleMetadata) Validate() error {
	if strings.TrimSpace(m.Title) == "" || len(m.Title) > 1024 {
		return invalid("标题不能为空，且不能超过 1024 字节")
	}
	u, err := url.Parse(m.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" ||
		strings.TrimSpace(m.URL) != m.URL || len(m.URL) > 2048 {
		return invalid("请填写有效的 http 或 https 原文网址，且不能超过 2048 字节")
	}
	if len(m.Date) == 10 {
		date, err := time.Parse("2006-01-02", m.Date)
		if err != nil || date.Format("2006-01-02") != m.Date {
			return invalid("发布日期须为 YYYY-MM-DD 或带时区的 RFC 3339 时间")
		}
	} else if _, err := time.Parse(time.RFC3339, m.Date); err != nil {
		return invalid("发布日期须为 YYYY-MM-DD 或带时区的 RFC 3339 时间")
	}
	if len(m.Author) > 256 && strings.TrimSpace(m.Author) != "" {
		return invalid("作者不能超过 256 字节")
	}
	return nil
}
