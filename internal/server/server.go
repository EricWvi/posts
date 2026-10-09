// Package server exposes the library over a JSON API and serves the
// embedded frontend.
package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"

	"posts/internal/auth"
	"posts/internal/library"
	"posts/internal/store"
)

// Options configures the server.
type Options struct {
	Version string
	// MaxUploadBytes caps one uploaded html file.
	MaxUploadBytes int64
}

type server struct {
	store *store.Store
	lib   *library.Library
	opts  Options
}

// New returns the HTTP handler for auth, the API and the frontend in web.
// The API requires a signed-in user; the app shell does not, so it can
// send the user to the login page.
func New(st *store.Store, lib *library.Library, authn *auth.Service, web fs.FS, opts Options) (http.Handler, error) {
	s := &server{store: st, lib: lib, opts: opts}
	static, err := newStatic(web)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/version", s.getVersion)
	authn.Register(mux)

	// Everything registered on private is only reachable with a session.
	private := http.NewServeMux()
	mux.Handle("/api/", authn.Require(private))
	mux.Handle("/", static)

	private.HandleFunc("GET /api/me", s.getMe)
	private.HandleFunc("GET /api/posts", s.listPosts)
	private.HandleFunc("POST /api/posts", s.uploadPosts)
	private.HandleFunc("GET /api/posts/{id}", s.getPost)
	private.HandleFunc("DELETE /api/posts/{id}", s.deletePost)
	private.HandleFunc("POST /api/posts/{id}/retry", s.retryPost)
	private.HandleFunc("GET /api/posts/{id}/md", s.getMarkdown)
	private.HandleFunc("GET /api/posts/{id}/zip", s.getZip)
	private.HandleFunc("GET /api/posts/{id}/assets/{name}", s.getAsset)
	private.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "接口不存在")
	})
	return mux, nil
}

func userID(r *http.Request) int64 { return auth.User(r.Context()).ID }

func (s *server) getVersion(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": s.opts.Version})
}

func (s *server) getMe(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, auth.User(r.Context()))
}

func (s *server) listPosts(w http.ResponseWriter, r *http.Request) {
	posts, err := s.store.Posts(r.Context(), userID(r))
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, posts)
}

// uploadResult reports each file of a multi-file upload on its own, so one
// bad file does not reject the rest.
type uploadResult struct {
	Posts  []store.Post  `json:"posts"`
	Errors []uploadError `json:"errors"`
}

type uploadError struct {
	File  string `json:"file"`
	Error string `json:"error"`
}

// uploadPosts accepts multipart/form-data with one or more "files" parts.
func (s *server) uploadPosts(w http.ResponseWriter, r *http.Request) {
	mr, err := r.MultipartReader()
	if err != nil {
		writeError(w, http.StatusBadRequest, "请使用 multipart/form-data 上传")
		return
	}
	res := uploadResult{Posts: []store.Post{}, Errors: []uploadError{}}
	for {
		part, err := mr.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writeError(w, http.StatusBadRequest, "读取上传内容失败")
			return
		}
		if part.FormName() != "files" || part.FileName() == "" {
			part.Close()
			continue
		}
		post, err := s.uploadOne(r, part)
		part.Close()
		if err != nil {
			res.Errors = append(res.Errors, uploadError{File: part.FileName(), Error: err.Error()})
			continue
		}
		res.Posts = append(res.Posts, post)
	}
	if len(res.Posts) == 0 && len(res.Errors) == 0 {
		writeError(w, http.StatusBadRequest, "没有收到文件")
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *server) uploadOne(r *http.Request, part *multipart.Part) (store.Post, error) {
	name := part.FileName()
	ext := strings.ToLower(path.Ext(name))
	if ext != ".html" && ext != ".htm" {
		return store.Post{}, errors.New("只支持 .html 文件")
	}
	data, err := io.ReadAll(io.LimitReader(part, s.opts.MaxUploadBytes+1))
	if err != nil {
		return store.Post{}, errors.New("读取文件失败")
	}
	if int64(len(data)) > s.opts.MaxUploadBytes {
		return store.Post{}, fmt.Errorf("文件超过 %d MB", s.opts.MaxUploadBytes>>20)
	}
	post, err := s.lib.Upload(r.Context(), userID(r), data, strings.TrimSuffix(name, path.Ext(name)))
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			return store.Post{}, errors.New(detail(err, store.ErrInvalid))
		}
		slog.Error("upload", "file", name, "err", err)
		return store.Post{}, errors.New("保存文件失败")
	}
	return post, nil
}

func (s *server) post(w http.ResponseWriter, r *http.Request) (store.Post, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || id <= 0 {
		writeError(w, http.StatusBadRequest, "无效的 id")
		return store.Post{}, false
	}
	p, err := s.store.Post(r.Context(), userID(r), id)
	if err != nil {
		writeStoreError(w, r, err)
		return store.Post{}, false
	}
	return p, true
}

func (s *server) getPost(w http.ResponseWriter, r *http.Request) {
	p, ok := s.post(w, r)
	if !ok {
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, p)
}

func (s *server) deletePost(w http.ResponseWriter, r *http.Request) {
	p, ok := s.post(w, r)
	if !ok {
		return
	}
	if err := s.lib.Delete(r.Context(), userID(r), p.ID); err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) retryPost(w http.ResponseWriter, r *http.Request) {
	p, ok := s.post(w, r)
	if !ok {
		return
	}
	if err := s.lib.Retry(r.Context(), userID(r), p.ID); err != nil {
		writeStoreError(w, r, err)
		return
	}
	if p, err := s.store.Post(r.Context(), userID(r), p.ID); err == nil {
		writeJSON(w, http.StatusOK, p)
	} else {
		writeStoreError(w, r, err)
	}
}

func (s *server) getMarkdown(w http.ResponseWriter, r *http.Request) {
	p, ok := s.post(w, r)
	if !ok {
		return
	}
	file, err := s.lib.MarkdownFile(p)
	if err != nil {
		writeStoreError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	serveFile(w, r, file)
}

func (s *server) getZip(w http.ResponseWriter, r *http.Request) {
	p, ok := s.post(w, r)
	if !ok {
		return
	}
	if _, err := s.lib.MarkdownFile(p); err != nil {
		writeStoreError(w, r, err)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "application/zip")
	h.Set("Content-Disposition", "attachment; filename*=UTF-8''"+url.PathEscape(p.Slug+".zip"))
	h.Set("Cache-Control", "no-store")
	if err := s.lib.WriteZip(w, p); err != nil {
		// Headers are gone; the truncated archive fails to open.
		slog.Error("write zip", "id", p.ID, "err", err)
	}
}

// getAsset serves a converted post's media. Names are content hashes, so
// the response never changes and may be cached for a year. It is private
// because it requires a session.
func (s *server) getAsset(w http.ResponseWriter, r *http.Request) {
	p, ok := s.post(w, r)
	if !ok {
		return
	}
	file, err := s.lib.AssetFile(p, r.PathValue("name"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	h := w.Header()
	h.Set("Cache-Control", "private, max-age=31536000, immutable")
	h.Set("X-Content-Type-Options", "nosniff")
	// Saved SVGs may contain scripts; never let them run.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	serveFile(w, r, file)
}

func serveFile(w http.ResponseWriter, r *http.Request, file string) {
	if _, err := os.Stat(file); err != nil {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, file)
}

func writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrInvalid):
		writeError(w, http.StatusBadRequest, detail(err, store.ErrInvalid))
	case errors.Is(err, store.ErrConflict):
		writeError(w, http.StatusConflict, detail(err, store.ErrConflict))
	case errors.Is(err, store.ErrNotFound):
		writeError(w, http.StatusNotFound, "文章不存在或已被删除")
	default:
		slog.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
		writeError(w, http.StatusInternalServerError, "服务器内部错误")
	}
}

// detail strips the sentinel prefix from a wrapped store error.
func detail(err, sentinel error) string {
	return strings.TrimPrefix(err.Error(), sentinel.Error()+": ")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}
