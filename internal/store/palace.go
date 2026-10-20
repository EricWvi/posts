package store

import (
	"context"
	"database/sql"
)

// EnqueueImport checks ownership and queues only an unimported, converted post.
func (s *Store) EnqueueImport(ctx context.Context, userID, id int64) error {
	return s.inTx(ctx, func(tx *sql.Tx) error {
		p, err := scanPost(tx.QueryRowContext(ctx, `SELECT `+postColumns+` FROM posts WHERE id = ? AND user_id = ?`, id, userID))
		if err != nil {
			return err
		}
		if p.Status != StatusDone || (p.PalaceStatus != "" && p.PalaceStatus != "failed") {
			return conflict("只有已转换且尚未导入的文章可以导入 Palace")
		}
		_, err = tx.ExecContext(ctx, `UPDATE posts SET palace_status = 'queued', palace_error = '' WHERE id = ?`, id)
		return err
	})
}

// RequeueImports resumes only imports previously requested by a user.
func (s *Store) RequeueImports(ctx context.Context) error {
	_, err := s.db.ExecContext(ctx, `UPDATE posts SET palace_status = 'queued' WHERE palace_status = 'importing'`)
	return err
}

func (s *Store) ClaimImport(ctx context.Context) (Post, error) {
	var p Post
	err := s.inTx(ctx, func(tx *sql.Tx) error {
		var err error
		p, err = scanPost(tx.QueryRowContext(ctx, `SELECT `+postColumns+` FROM posts WHERE status = ? AND palace_status = 'queued' ORDER BY id LIMIT 1`, StatusDone))
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `UPDATE posts SET palace_status = 'importing' WHERE id = ?`, p.ID)
		p.PalaceStatus = "importing"
		return err
	})
	return p, err
}

func (s *Store) FinishImport(ctx context.Context, id int64, palaceID, reason string) error {
	status := "done"
	if reason != "" {
		status = "failed"
	}
	res, err := s.db.ExecContext(ctx, `UPDATE posts SET palace_status = ?, palace_id = ?, palace_error = ? WHERE id = ? AND palace_status = 'importing'`, status, palaceID, reason, id)
	if err != nil {
		return err
	}
	if n, err := res.RowsAffected(); err != nil {
		return err
	} else if n == 0 {
		return conflict("文章导入状态已变化")
	}
	return nil
}
