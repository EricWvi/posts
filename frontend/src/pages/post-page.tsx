import { ArrowLeft, Download, ExternalLink, TriangleAlert } from "lucide-react";
import { Link, useLocation } from "wouter";

import { ConfirmDelete } from "@/components/confirm-delete";
import { MarkdownView } from "@/components/markdown-view";
import { RetryButton } from "@/components/retry-button";
import { StatusBadge } from "@/components/status-badge";
import { Button } from "@/components/ui/button";
import { useMarkdown, usePost } from "@/hooks/use-posts";
import { zipUrl, type ContentWarning } from "@/lib/api";
import { formatDateTime, hostOf } from "@/lib/format";

export function PostPage({ id }: { id: number }) {
  const [, navigate] = useLocation();
  const { data: post, error } = usePost(id);
  const md = useMarkdown(post);

  if (error) return <Message>{error.message}</Message>;
  if (!post) return <Message>加载中…</Message>;

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-center gap-2">
        <Button variant="ghost" asChild>
          <Link href="/">
            <ArrowLeft />
            返回
          </Link>
        </Button>
        <div className="flex-1" />
        {post.status === "done" && (
          <Button variant="outline" asChild>
            <a href={zipUrl(post.id)} download>
              <Download />
              下载 zip
            </a>
          </Button>
        )}
        {post.status === "failed" && <RetryButton id={post.id} />}
        <ConfirmDelete post={post} onDeleted={() => navigate("/", { replace: true })} />
      </div>

      <header className="space-y-2 border-b pb-4">
        <p className="flex flex-wrap items-center gap-x-3 gap-y-1 text-sm text-muted-foreground">
          {post.publishedDate && <span>发布于 {post.publishedDate}</span>}
          {post.sourceUrl && (
            <a href={post.sourceUrl} target="_blank" rel="noreferrer" className="inline-flex items-center gap-1 hover:underline">
              {hostOf(post.sourceUrl)}
              <ExternalLink className="size-3.5" />
            </a>
          )}
          <span>上传于 {formatDateTime(post.uploadedAt)}</span>
        </p>
        {post.path && <p className="break-all font-mono text-xs text-muted-foreground">{post.path}</p>}
      </header>

      {post.warning && <WarningPanel warning={post.warning} />}

      {post.status === "done" ? (
        md.data !== undefined ? (
          <MarkdownView id={post.id} markdown={md.data} />
        ) : md.error ? (
          <Message>{md.error.message}</Message>
        ) : (
          <Message>加载中…</Message>
        )
      ) : (
        <div className="space-y-3 rounded-xl border bg-card px-6 py-10 text-center">
          <h1 className="text-lg font-medium">{post.title}</h1>
          <StatusBadge status={post.status} />
          {post.status === "failed" ? (
            <p className="whitespace-pre-wrap break-words text-sm text-destructive">{post.error}</p>
          ) : (
            <p className="text-sm text-muted-foreground">转换通常需要一两分钟，完成后自动显示。</p>
          )}
        </div>
      )}
    </div>
  );
}

function WarningPanel({ warning }: { warning: ContentWarning }) {
  return (
    <details className="rounded-xl border border-amber-500/40 bg-amber-500/10 px-4 py-3 text-sm">
      <summary className="flex cursor-pointer items-center gap-2 font-medium">
        <TriangleAlert className="size-4 shrink-0 text-amber-600 dark:text-amber-400" />
        内容校验：{warning.checked} 段文字中有 {warning.unmatchedCount} 段在原网页中找不到，可能被模型改写
      </summary>
      <ul className="mt-3 space-y-2">
        {warning.unmatched.map((text, i) => (
          <li key={i} className="whitespace-pre-wrap break-words rounded-md bg-background/60 px-3 py-2 font-mono text-xs">
            {text}
          </li>
        ))}
      </ul>
      {warning.unmatchedCount > warning.unmatched.length && (
        <p className="mt-2 text-xs text-muted-foreground">只列出前 {warning.unmatched.length} 段。</p>
      )}
    </details>
  );
}

function Message({ children }: { children: React.ReactNode }) {
  return <p className="py-16 text-center text-sm text-muted-foreground">{children}</p>;
}
