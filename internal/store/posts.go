package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"
)

// Status is where a post is in its life cycle:
//
//	uploading → queued → converting → done
//	                 ↖               ↘
//	                   ─── failed ←──
//
// A post is uploading only while its files are being written; one left in
// that state was cut off by a restart.
type Status string

const (
	StatusUploading  Status = "uploading"
	StatusQueued     Status = "queued"
	StatusConverting Status = "converting"
	StatusDone       Status = "done"
	StatusFailed     Status = "failed"
)

// Post is an uploaded page and the outcome of its conversion.
type Post struct {
	ID     int64  `json:"id"`
	UserID int64  `json:"-"`
	Status Status `json:"status"`
	// Title is the page's <title> until the conversion names the article.
	Title     string     `json:"title"`
	SourceURL string     `json:"sourceUrl"`
	SavedAt   *time.Time `json:"savedAt"`
	// PublishedDate (YYYY-MM-DD), Slug and Path are set by the conversion.
	// Path is the Markdown file relative to the data directory.
	PublishedDate string `json:"publishedDate"`
	Slug          string `json:"slug"`
	Path          string `json:"path"`
	// Error says why the last conversion failed.
	Error string `json:"error"`
	// Warning is the content check report when the Markdown contains text
	// the page does not, or nil.
	Warning     json.RawMessage `json:"warning"`
	UploadedAt  time.Time       `json:"uploadedAt"`
	ConvertedAt *time.Time      `json:"convertedAt"`
}

// NewPost describes an upload.
type NewPost struct {
	Title     string
	SourceURL string
	SavedAt   time.Time // zero when unknown
}

// Converted is the result of a successful conversion.
type Converted struct {
	Title         string
	PublishedDate string
	Slug          string
	Path          string
	Warning       json.RawMessage // nil when the content check passed
}

const postColumns = `id, user_id, status, title, source_url, saved_at, published_date, slug, path,
	error, warning, uploaded_at, converted_at`

func scanPost(row interface{ Scan(...any) error }) (Post, error) {
	var p Post
	var savedAt, convertedAt sql.NullInt64
	var uploadedAt int64
	var warning string
	err := row.Scan(&p.ID, &p.UserID, &p.Status, &p.Title, &p.SourceURL, &savedAt, &p.PublishedDate,
		&p.Slug, &p.Path, &p.Error, &warning, &uploadedAt, &convertedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return p, ErrNotFound
	}
	p.UploadedAt = time.Unix(uploadedAt, 0).UTC()
	p.SavedAt = unixPtr(savedAt)
	p.ConvertedAt = unixPtr(convertedAt)
	if warning != "" {
		p.Warning = json.RawMessage(warning)
	}
	return p, err
}

func unixPtr(v sql.NullInt64) *time.Time {
	if !v.Valid {
		return nil
	}
	t := time.Unix(v.Int64, 0).UTC()
	return &t
}

// CreatePost records an upload in the uploading state.
func (s *Store) CreatePost(ctx context.Context, userID int64, in NewPost) (Post, error) {
	var savedAt sql.NullInt64
	if !in.SavedAt.IsZero() {
		savedAt = sql.NullInt64{Int64: in.SavedAt.Unix(), Valid: true}
	}
	res, err := s.db.ExecContext(ctx,
		`INSERT INTO posts (user_id, status, title, source_url, saved_at, uploaded_at) VALUES (?, ?, ?, ?, ?, ?)`,
		userID, StatusUploading, in.Title, in.SourceURL, savedAt, time.Now().Unix())
	if err != nil {
		return Post{}, err
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Post{}, err
	}
	return s.post(ctx, `WHERE id = ?`, id)
}

// Enqueue moves a post whose files are in place from uploading to queued.
func (s *Store) Enqueue(ctx context.Context, id int64) error {
	return s.transition(ctx, id, StatusUploading, StatusQueued, `status = ?`, StatusQueued)
}

// AbandonedUploads lists the posts a restart cut off while uploading.
func (s *Store) AbandonedUploads(ctx context.Context) ([]Post, error) {
	return s.posts(ctx, `WHERE status = ? ORDER BY id`, StatusUploading)
}

// RequeueConverting puts conversions a restart interrupted back in the
// queue and reports how many there were.
func (s *Store) RequeueConverting(ctx context.Context) (int64, error) {
	res, err := s.db.ExecContext(ctx, `UPDATE posts SET status = ? WHERE status = ?`, StatusQueued, StatusConverting)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// ClaimNext marks the oldest queued post as converting and returns it, or
// returns ErrNotFound when the queue is empty.
func (s *Store) ClaimNext(ctx context.Context) (Post, error) {
	var p Post
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		p, err = scanPost(tx.QueryRowContext(ctx,
			`SELECT `+postColumns+` FROM posts WHERE status = ? ORDER BY id LIMIT 1`, StatusQueued))
		if err != nil {
			return err
		}
		p.Status = StatusConverting
		_, err = tx.ExecContext(ctx, `UPDATE posts SET status = ? WHERE id = ?`, StatusConverting, p.ID)
		return err
	})
	return p, err
}

// Finish records a successful conversion.
func (s *Store) Finish(ctx context.Context, id int64, c Converted) error {
	return s.transition(ctx, id, StatusConverting, StatusDone,
		`status = ?, title = ?, published_date = ?, slug = ?, path = ?, warning = ?, error = '', converted_at = ?`,
		StatusDone, c.Title, c.PublishedDate, c.Slug, c.Path, string(c.Warning), time.Now().Unix())
}

// Fail records why a conversion failed.
func (s *Store) Fail(ctx context.Context, id int64, reason string) error {
	return s.transition(ctx, id, StatusConverting, StatusFailed, `status = ?, error = ?`, StatusFailed, reason)
}

// Retry queues a failed post again.
func (s *Store) Retry(ctx context.Context, userID, id int64) error {
	p, err := s.Post(ctx, userID, id)
	if err != nil {
		return err
	}
	if p.Status != StatusFailed {
		return conflict("只有转换失败的文章可以重试")
	}
	return s.transition(ctx, id, StatusFailed, StatusQueued, `status = ?, error = ''`, StatusQueued)
}

// transition applies set to the post if it is in state from.
func (s *Store) transition(ctx context.Context, id int64, from, to Status, set string, args ...any) error {
	res, err := s.db.ExecContext(ctx, `UPDATE posts SET `+set+` WHERE id = ? AND status = ?`,
		append(args, id, from)...)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return conflict("文章状态已变化，无法改为 " + string(to))
	}
	return nil
}

// Posts lists the user's posts, newest upload first.
func (s *Store) Posts(ctx context.Context, userID int64) ([]Post, error) {
	return s.posts(ctx, `WHERE user_id = ? ORDER BY id DESC`, userID)
}

// Post returns one of the user's posts.
func (s *Store) Post(ctx context.Context, userID, id int64) (Post, error) {
	return s.post(ctx, `WHERE id = ? AND user_id = ?`, id, userID)
}

// DeletePost removes one of the user's posts and returns it so its files
// can be removed. A post being converted cannot be deleted.
func (s *Store) DeletePost(ctx context.Context, userID, id int64) (Post, error) {
	var p Post
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		p, err = scanPost(tx.QueryRowContext(ctx,
			`SELECT `+postColumns+` FROM posts WHERE id = ? AND user_id = ?`, id, userID))
		if err != nil {
			return err
		}
		if p.Status == StatusConverting || p.Status == StatusUploading {
			return conflict("文章正在处理，请稍后再删除")
		}
		_, err = tx.ExecContext(ctx, `DELETE FROM posts WHERE id = ?`, id)
		return err
	})
	return p, err
}

// RemoveAbandoned deletes a post left in the uploading state.
func (s *Store) RemoveAbandoned(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM posts WHERE id = ? AND status = ?`, id, StatusUploading)
	return err
}

func (s *Store) post(ctx context.Context, where string, args ...any) (Post, error) {
	return scanPost(s.db.QueryRowContext(ctx, `SELECT `+postColumns+` FROM posts `+where, args...))
}

func (s *Store) posts(ctx context.Context, where string, args ...any) ([]Post, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+postColumns+` FROM posts `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Post{}
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		list = append(list, p)
	}
	return list, rows.Err()
}
