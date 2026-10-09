// Package library keeps the posts on disk and converts them one at a time.
//
// Layout under the data directory:
//
//	.work/<id>/index.html         cleaned page waiting for (or failed) conversion
//	.work/<id>/assets/<name>      media extracted from the page
//	<year>/<month>/<id>/<slug>.md converted post
//	<year>/<month>/<id>/assets/   the media the Markdown references
//
// Year and month come from the post's publication date, so a post only gets
// its final place once converted. The work directory is removed after a
// successful conversion and kept after a failed one, so it can be retried.
package library

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"posts/internal/convert"
	"posts/internal/singlefile"
	"posts/internal/store"
	"posts/internal/verify"
)

// Converter turns cleaned html into an article.
type Converter interface {
	Convert(ctx context.Context, html []byte) (convert.Article, error)
}

// Library is safe for concurrent use. Run must be running for queued posts
// to be converted.
type Library struct {
	store *store.Store
	dir   string
	conv  Converter
	wake  chan struct{}
}

// New prepares the data directory, removes uploads a restart cut off and
// requeues conversions it interrupted.
func New(ctx context.Context, st *store.Store, dir string, conv Converter) (*Library, error) {
	l := &Library{store: st, dir: dir, conv: conv, wake: make(chan struct{}, 1)}
	if err := os.MkdirAll(l.workRoot(), 0o755); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	abandoned, err := st.AbandonedUploads(ctx)
	if err != nil {
		return nil, err
	}
	for _, p := range abandoned {
		if err := os.RemoveAll(l.workDir(p.ID)); err != nil {
			return nil, err
		}
		if err := st.RemoveAbandoned(ctx, p.ID); err != nil {
			return nil, err
		}
	}
	n, err := st.RequeueConverting(ctx)
	if err != nil {
		return nil, err
	}
	if n > 0 {
		slog.Info("requeued interrupted conversions", "count", n)
	}
	return l, nil
}

func (l *Library) workRoot() string { return filepath.Join(l.dir, ".work") }
func (l *Library) workDir(id int64) string {
	return filepath.Join(l.workRoot(), strconv.FormatInt(id, 10))
}

// Upload cleans a SingleFile page, stores it and queues its conversion.
// fallbackTitle names the post when the page has no <title>.
func (l *Library) Upload(ctx context.Context, userID int64, src []byte, fallbackTitle string) (store.Post, error) {
	page, err := singlefile.Clean(src)
	if err != nil {
		return store.Post{}, fmt.Errorf("%w: 无法解析 html", store.ErrInvalid)
	}
	title := page.Title
	if title == "" {
		title = fallbackTitle
	}
	p, err := l.store.CreatePost(ctx, userID, store.NewPost{Title: title, SourceURL: page.SourceURL, SavedAt: page.SavedAt})
	if err != nil {
		return store.Post{}, err
	}
	if err := l.writeWork(p.ID, page); err != nil {
		os.RemoveAll(l.workDir(p.ID))
		l.store.RemoveAbandoned(context.WithoutCancel(ctx), p.ID)
		return store.Post{}, err
	}
	if err := l.store.Enqueue(ctx, p.ID); err != nil {
		return store.Post{}, err
	}
	l.notify()
	p.Status = store.StatusQueued
	return p, nil
}

func (l *Library) writeWork(id int64, page singlefile.Page) error {
	assets := filepath.Join(l.workDir(id), singlefile.AssetDir)
	if err := os.MkdirAll(assets, 0o755); err != nil {
		return err
	}
	for _, a := range page.Assets {
		if err := os.WriteFile(filepath.Join(assets, a.Name), a.Data, 0o644); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(l.workDir(id), "index.html"), page.HTML, 0o644)
}

// Retry queues a failed post again.
func (l *Library) Retry(ctx context.Context, userID, id int64) error {
	if err := l.store.Retry(ctx, userID, id); err != nil {
		return err
	}
	l.notify()
	return nil
}

// Delete removes a post and its files.
func (l *Library) Delete(ctx context.Context, userID, id int64) error {
	p, err := l.store.DeletePost(ctx, userID, id)
	if err != nil {
		return err
	}
	if p.Path != "" {
		if err := os.RemoveAll(l.postDir(p)); err != nil {
			slog.Error("remove post dir", "id", id, "err", err)
		}
	}
	if err := os.RemoveAll(l.workDir(id)); err != nil {
		slog.Error("remove work dir", "id", id, "err", err)
	}
	return nil
}

func (l *Library) notify() {
	select {
	case l.wake <- struct{}{}:
	default:
	}
}

// postDir is the directory of a converted post.
func (l *Library) postDir(p store.Post) string {
	return filepath.Join(l.dir, filepath.Dir(filepath.FromSlash(p.Path)))
}

// MarkdownFile is the path of a converted post's Markdown.
func (l *Library) MarkdownFile(p store.Post) (string, error) {
	if p.Status != store.StatusDone || p.Path == "" {
		return "", fmt.Errorf("%w: 文章尚未转换完成", store.ErrConflict)
	}
	return filepath.Join(l.dir, filepath.FromSlash(p.Path)), nil
}

var assetName = regexp.MustCompile(`^[0-9a-f]{16}\.[a-z0-9]+$`)

// AssetFile is the path of one of a converted post's assets.
func (l *Library) AssetFile(p store.Post, name string) (string, error) {
	if p.Status != store.StatusDone || !assetName.MatchString(name) {
		return "", store.ErrNotFound
	}
	return filepath.Join(l.postDir(p), singlefile.AssetDir, name), nil
}

// WriteZip writes a converted post as a zip archive holding a <slug>/
// directory with the Markdown and its assets.
func (l *Library) WriteZip(w io.Writer, p store.Post) error {
	md, err := l.MarkdownFile(p)
	if err != nil {
		return err
	}
	zw := zip.NewWriter(w)
	add := func(src, name string) error {
		f, err := os.Open(src)
		if err != nil {
			return err
		}
		defer f.Close()
		info, err := f.Stat()
		if err != nil {
			return err
		}
		hdr, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		hdr.Name = p.Slug + "/" + name
		hdr.Method = zip.Deflate
		dst, err := zw.CreateHeader(hdr)
		if err != nil {
			return err
		}
		_, err = io.Copy(dst, f)
		return err
	}
	if err := add(md, filepath.Base(md)); err != nil {
		return err
	}
	assets := filepath.Join(l.postDir(p), singlefile.AssetDir)
	entries, err := os.ReadDir(assets)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	for _, e := range entries {
		if err := add(filepath.Join(assets, e.Name()), singlefile.AssetDir+"/"+e.Name()); err != nil {
			return err
		}
	}
	return zw.Close()
}

// Run converts queued posts one at a time until ctx is done.
func (l *Library) Run(ctx context.Context) {
	for {
		p, err := l.store.ClaimNext(ctx)
		switch {
		case ctx.Err() != nil:
			return
		case errors.Is(err, store.ErrNotFound):
			select {
			case <-l.wake:
			case <-ctx.Done():
				return
			}
		case err != nil:
			slog.Error("claim next post", "err", err)
			select {
			case <-time.After(5 * time.Second):
			case <-ctx.Done():
				return
			}
		default:
			l.process(ctx, p)
		}
	}
}

func (l *Library) process(ctx context.Context, p store.Post) {
	log := slog.With("id", p.ID)
	log.Info("converting", "title", p.Title)
	started := time.Now()
	converted, err := l.convert(ctx, p)
	if ctx.Err() != nil {
		// Shutting down: the post stays converting and is requeued on start.
		return
	}
	if err != nil {
		log.Error("conversion failed", "err", err)
		if err := l.store.Fail(context.WithoutCancel(ctx), p.ID, err.Error()); err != nil {
			log.Error("record failure", "err", err)
		}
		return
	}
	if err := l.store.Finish(context.WithoutCancel(ctx), p.ID, converted); err != nil {
		log.Error("record conversion", "err", err)
		os.RemoveAll(filepath.Join(l.dir, filepath.Dir(filepath.FromSlash(converted.Path))))
		l.store.Fail(context.WithoutCancel(ctx), p.ID, "无法保存转换结果")
		return
	}
	os.RemoveAll(l.workDir(p.ID))
	log.Info("converted", "path", converted.Path, "warning", converted.Warning != nil, "took", time.Since(started).Round(time.Second))
}

// convert runs the conversion and writes the post to its final directory.
func (l *Library) convert(ctx context.Context, p store.Post) (store.Converted, error) {
	work := l.workDir(p.ID)
	html, err := os.ReadFile(filepath.Join(work, "index.html"))
	if err != nil {
		return store.Converted{}, fmt.Errorf("读取待转换页面: %w", err)
	}
	article, err := l.conv.Convert(ctx, html)
	if err != nil {
		return store.Converted{}, err
	}
	report, err := verify.Check(html, article.Markdown)
	if err != nil {
		return store.Converted{}, fmt.Errorf("内容校验: %w", err)
	}
	var warning json.RawMessage
	if !report.OK() {
		if warning, err = json.Marshal(report); err != nil {
			return store.Converted{}, err
		}
	}

	rel := fmt.Sprintf("%s/%s/%d", article.PublishedDate[:4], article.PublishedDate[5:7], p.ID)
	dir := filepath.Join(l.dir, filepath.FromSlash(rel))
	// A previous attempt may have been cut off half way.
	if err := os.RemoveAll(dir); err != nil {
		return store.Converted{}, err
	}
	if err := l.place(work, dir, article); err != nil {
		os.RemoveAll(dir)
		return store.Converted{}, fmt.Errorf("保存转换结果: %w", err)
	}
	return store.Converted{
		Title:         article.Title,
		PublishedDate: article.PublishedDate,
		Slug:          article.Slug,
		Path:          rel + "/" + article.Slug + ".md",
		Warning:       warning,
	}, nil
}

var assetRef = regexp.MustCompile(singlefile.AssetDir + `/([0-9a-f]{16}\.[a-z0-9]+)`)

// place writes the Markdown into dir together with the assets it
// references. Assets the conversion dropped, such as site logos, are left
// behind.
func (l *Library) place(work, dir string, article convert.Article) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for _, m := range assetRef.FindAllStringSubmatch(article.Markdown, -1) {
		src := filepath.Join(work, singlefile.AssetDir, m[1])
		dst := filepath.Join(dir, singlefile.AssetDir, m[1])
		if _, err := os.Stat(dst); err == nil {
			continue
		}
		if _, err := os.Stat(src); errors.Is(err, os.ErrNotExist) {
			continue // a path the model made up; the link stays broken
		}
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		if err := copyFile(src, dst); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(dir, article.Slug+".md"), []byte(article.Markdown), 0o644)
}

// copyFile hard-links src to dst, copying when the file system refuses.
// The work directory keeps its own copy until the conversion is recorded.
func copyFile(src, dst string) error {
	if os.Link(src, dst) == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
