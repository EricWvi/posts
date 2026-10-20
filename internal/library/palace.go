package library

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"posts/internal/store"
)

// Import queues an explicit user request. Conversion never calls this method.
func (l *Library) Import(ctx context.Context, userID, id int64, metadata store.ArticleMetadata) error {
	if _, err := l.store.Post(ctx, userID, id); err != nil {
		return err
	}
	if l.importer == nil {
		return fmt.Errorf("%w: 尚未配置 Palace 导入", store.ErrConflict)
	}
	if err := l.store.EnqueueImport(ctx, userID, id, metadata); err != nil {
		return err
	}
	l.notify()
	return nil
}

func (l *Library) importPost(ctx context.Context, p store.Post) {
	id, err := l.uploadArticle(ctx, p)
	if ctx.Err() != nil {
		// Keep importing for recovery on restart; the source URL makes retry idempotent.
		return
	}
	reason := ""
	if err != nil {
		reason = err.Error()
		slog.Error("Palace import failed", "id", p.ID, "err", err)
	}
	if err := l.store.FinishImport(context.WithoutCancel(ctx), p.ID, id, reason); err != nil {
		slog.Error("record Palace import", "id", p.ID, "err", err)
	}
}

func (l *Library) uploadArticle(ctx context.Context, p store.Post) (string, error) {
	metadata := p.ImportMetadata()
	if err := metadata.Validate(); err != nil {
		return "", err
	}
	data, err := json.Marshal(metadata)
	if err != nil {
		return "", err
	}
	dir := l.postDir(p)
	if err := os.WriteFile(filepath.Join(dir, "article.json"), append(data, '\n'), 0o644); err != nil {
		return "", fmt.Errorf("保存 article.json: %w", err)
	}
	return l.importer.Upload(ctx, dir)
}
