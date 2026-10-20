import { Upload } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { useImport } from "@/hooks/use-posts";
import type { Post } from "@/lib/api";

export function PalaceImport({ post }: { post: Post }) {
  const mutation = useImport();
  if (post.status !== "done") return null;
  if (post.palaceStatus === "done") {
    return <span className="shrink-0 text-xs text-muted-foreground">已导入 Palace</span>;
  }
  const busy = post.palaceStatus === "queued" || post.palaceStatus === "importing";
  const label =
    post.palaceStatus === "queued"
      ? "等待导入"
      : post.palaceStatus === "importing"
        ? "导入中…"
        : post.palaceStatus === "failed"
          ? "重试导入 Palace"
          : "导入 Palace";
  return (
    <Button
      variant="outline"
      size="sm"
      disabled={busy || mutation.isPending}
      onClick={() => mutation.mutate(post.id, { onError: (err) => toast.error(err.message) })}
    >
      <Upload />
      {label}
    </Button>
  );
}
