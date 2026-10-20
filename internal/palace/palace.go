// Package palace imports local article directories with the installed pal CLI.
package palace

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

type Client struct {
	command string
	timeout time.Duration
}

func New() *Client { return &Client{command: "pal", timeout: 5 * time.Minute} }

// Upload inherits PAL_URL and PAL_TOKEN; credentials never appear in arguments.
func (c *Client) Upload(ctx context.Context, dir string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, c.command, "article", "upload", dir)
	cmd.WaitDelay = time.Second
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return "", fmt.Errorf("Palace 导入中断或超时: %w", ctx.Err())
		}
		detail := strings.TrimSpace(stderr.String())
		if token := os.Getenv("PAL_TOKEN"); token != "" {
			detail = strings.ReplaceAll(detail, token, "[redacted]")
		}
		if detail == "" {
			detail = err.Error()
		}
		return "", fmt.Errorf("Palace 导入失败: %s", detail)
	}
	var result struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil || strings.TrimSpace(result.ID) == "" {
		return "", fmt.Errorf("pal 未返回有效的文章 ID，可重试导入")
	}
	return result.ID, nil
}
