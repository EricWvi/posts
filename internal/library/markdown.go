package library

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"unicode/utf8"

	"posts/internal/store"
)

// SaveMarkdown replaces only the archived Markdown, before Palace import.
func (l *Library) SaveMarkdown(ctx context.Context, userID, id int64, data []byte) error {
	l.editMu.Lock()
	defer l.editMu.Unlock()
	p, err := l.store.Post(ctx, userID, id)
	if err != nil {
		return err
	}
	file, err := l.MarkdownFile(p)
	if err != nil {
		return err
	}
	if p.PalaceStatus != "" && p.PalaceStatus != "failed" {
		return fmt.Errorf("%w: 只有尚未导入 Palace 的文章可以编辑", store.ErrConflict)
	}
	if !utf8.Valid(data) {
		return fmt.Errorf("%w: Markdown 必须为 UTF-8 文本", store.ErrInvalid)
	}
	info, err := os.Stat(file)
	if err != nil {
		return err
	}
	// Rename within the same directory so readers never see a partial save.
	f, err := os.CreateTemp(filepath.Dir(file), ".markdown-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err := f.Chmod(info.Mode().Perm()); err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), file)
}
