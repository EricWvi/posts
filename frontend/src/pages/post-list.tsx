import { FileText, TriangleAlert } from "lucide-react";
import { Link } from "wouter";

import { ConfirmDelete } from "@/components/confirm-delete";
import { RetryButton } from "@/components/retry-button";
import { StatusBadge } from "@/components/status-badge";
import { UploadStatus } from "@/components/upload-zone";
import { usePosts } from "@/hooks/use-posts";
import type { Post } from "@/lib/api";
import { formatDateTime, hostOf } from "@/lib/format";

export function PostList() {
  const { data: posts, error, isPending } = usePosts();
  return (
    <div className="space-y-4">
      <UploadStatus />
      {isPending ? (
        <p className="py-16 text-center text-sm text-muted-foreground">加载中…</p>
      ) : error ? (
        <p className="py-16 text-center text-sm text-destructive">{error.message}</p>
      ) : posts.length === 0 ? (
        <Empty />
      ) : (
        <ul className="divide-y rounded-xl border bg-card">
          {posts.map((p) => (
            <PostRow key={p.id} post={p} />
          ))}
        </ul>
      )}
    </div>
  );
}

function Empty() {
  return (
    <div className="rounded-xl border border-dashed px-6 py-16 text-center">
      <FileText className="mx-auto mb-3 size-8 text-muted-foreground" />
      <p className="font-medium">还没有文章</p>
      <p className="mt-1 text-sm text-muted-foreground">
        用浏览器的 SingleFile 插件保存网页，再把 .html 文件拖到这里或点击“上传网页”。
      </p>
    </div>
  );
}

function PostRow({ post }: { post: Post }) {
  const meta = [
    post.sourceUrl && hostOf(post.sourceUrl),
    post.publishedDate ? `发布于 ${post.publishedDate}` : `上传于 ${formatDateTime(post.uploadedAt)}`,
  ].filter(Boolean);
  return (
    <li className="flex items-center gap-3 px-4 py-3">
      <div className="min-w-0 flex-1">
        <div className="flex items-center gap-2">
          {post.status === "done" ? (
            <Link href={`/posts/${post.id}`} className="truncate font-medium hover:underline">
              {post.title}
            </Link>
          ) : (
            <span className="truncate font-medium text-muted-foreground">{post.title}</span>
          )}
          {post.warning && (
            <TriangleAlert className="size-4 shrink-0 text-amber-600 dark:text-amber-400" aria-label="内容校验有警告" />
          )}
        </div>
        <p className="mt-0.5 truncate text-xs text-muted-foreground">{meta.join(" · ")}</p>
        {post.status === "failed" && <p className="mt-1 line-clamp-2 break-words text-xs text-destructive">{post.error}</p>}
      </div>
      {post.status !== "done" && <StatusBadge status={post.status} />}
      {post.status === "failed" && <RetryButton id={post.id} iconOnly />}
      <ConfirmDelete post={post} iconOnly />
    </li>
  );
}
