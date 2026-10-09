import { Trash2 } from "lucide-react";
import { toast } from "sonner";

import {
  AlertDialog,
  AlertDialogAction,
  AlertDialogCancel,
  AlertDialogContent,
  AlertDialogDescription,
  AlertDialogFooter,
  AlertDialogHeader,
  AlertDialogTitle,
  AlertDialogTrigger,
} from "@/components/ui/alert-dialog";
import { Button } from "@/components/ui/button";
import { useDelete } from "@/hooks/use-posts";
import type { Post } from "@/lib/api";
import { isPending } from "@/lib/format";

/** Delete button with a confirmation dialog. */
export function ConfirmDelete({ post, onDeleted, iconOnly }: { post: Post; onDeleted?: () => void; iconOnly?: boolean }) {
  const remove = useDelete();
  const busy = isPending(post.status);
  return (
    <AlertDialog>
      <AlertDialogTrigger asChild>
        <Button
          variant="ghost"
          size={iconOnly ? "icon" : "default"}
          disabled={busy || remove.isPending}
          title={busy ? "转换完成或失败后才能删除" : "删除"}
          aria-label="删除"
        >
          <Trash2 />
          {!iconOnly && "删除"}
        </Button>
      </AlertDialogTrigger>
      <AlertDialogContent>
        <AlertDialogHeader>
          <AlertDialogTitle>删除这篇文章？</AlertDialogTitle>
          <AlertDialogDescription>「{post.title}」的 Markdown 和图片等资源会一并删除，无法恢复。</AlertDialogDescription>
        </AlertDialogHeader>
        <AlertDialogFooter>
          <AlertDialogCancel>取消</AlertDialogCancel>
          <AlertDialogAction
            className="bg-destructive text-white hover:bg-destructive/90"
            onClick={() =>
              remove.mutate(post.id, {
                onSuccess: () => {
                  toast.success("已删除");
                  onDeleted?.();
                },
                onError: (err) => toast.error(err.message),
              })
            }
          >
            删除
          </AlertDialogAction>
        </AlertDialogFooter>
      </AlertDialogContent>
    </AlertDialog>
  );
}
