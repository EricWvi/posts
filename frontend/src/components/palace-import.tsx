import { Upload } from "lucide-react";
import { Dialog } from "radix-ui";
import { useId, useState, type FormEvent } from "react";

import { Button } from "@/components/ui/button";
import { useImport } from "@/hooks/use-posts";
import type { ArticleMetadata, Post } from "@/lib/api";

const inputClass = "h-10 w-full rounded-md border bg-background px-3 text-sm outline-none focus-visible:ring-2 focus-visible:ring-ring";

export function PalaceImport({ post }: { post: Post }) {
  const mutation = useImport();
  const [open, setOpen] = useState(false);
  const [metadata, setMetadata] = useState<ArticleMetadata>({ title: "", url: "", date: "", author: "" });
  const fieldID = useId();
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
  const changeOpen = (next: boolean) => {
    if (mutation.isPending) return;
    if (next) {
      setMetadata(post.palaceMetadata ?? { title: post.title, url: post.sourceUrl, date: post.publishedDate, author: post.author });
      mutation.reset();
    }
    setOpen(next);
  };
  const submit = (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    if (mutation.isPending || busy) return;
    mutation.mutate({ id: post.id, metadata }, { onSuccess: () => setOpen(false) });
  };
  return (
    <Dialog.Root open={open} onOpenChange={changeOpen}>
      <Dialog.Trigger asChild>
        <Button variant="outline" size="sm" disabled={busy || mutation.isPending}>
          <Upload />
          {label}
        </Button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/40" />
        <Dialog.Content
          aria-describedby={undefined}
          className="fixed left-1/2 top-1/2 z-50 max-h-[calc(100svh-2rem)] w-[calc(100%-2rem)] max-w-md -translate-x-1/2 -translate-y-1/2 overflow-y-auto rounded-xl border bg-popover p-6 text-popover-foreground shadow-lg"
        >
          <Dialog.Title className="mb-5 text-lg font-semibold">导入 Palace</Dialog.Title>
          <form onSubmit={submit} className="space-y-4">
            <fieldset disabled={mutation.isPending} className="space-y-4">
              <div className="space-y-1.5">
                <label htmlFor={`${fieldID}-title`} className="text-sm font-medium">
                  标题
                </label>
                <input
                  id={`${fieldID}-title`}
                  className={inputClass}
                  required
                  value={metadata.title}
                  onChange={(e) => setMetadata({ ...metadata, title: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <label htmlFor={`${fieldID}-url`} className="text-sm font-medium">
                  原文网址
                </label>
                <input
                  id={`${fieldID}-url`}
                  className={inputClass}
                  type="url"
                  required
                  placeholder="https://example.com/article"
                  value={metadata.url}
                  onChange={(e) => setMetadata({ ...metadata, url: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <label htmlFor={`${fieldID}-date`} className="text-sm font-medium">
                  发布日期
                </label>
                <input
                  id={`${fieldID}-date`}
                  className={inputClass}
                  required
                  placeholder="2026-10-20 或 2026-10-20T12:00:00+08:00"
                  value={metadata.date}
                  onChange={(e) => setMetadata({ ...metadata, date: e.target.value })}
                />
              </div>
              <div className="space-y-1.5">
                <label htmlFor={`${fieldID}-author`} className="text-sm font-medium">
                  作者（可选）
                </label>
                <input
                  id={`${fieldID}-author`}
                  className={inputClass}
                  value={metadata.author ?? ""}
                  onChange={(e) => setMetadata({ ...metadata, author: e.target.value })}
                />
              </div>
            </fieldset>
            {mutation.error && (
              <p role="alert" className="text-sm text-destructive">
                {mutation.error.message}
              </p>
            )}
            <div className="flex justify-end gap-2 pt-2">
              <Dialog.Close asChild>
                <Button type="button" variant="outline" disabled={mutation.isPending}>
                  取消
                </Button>
              </Dialog.Close>
              <Button type="submit" disabled={mutation.isPending || busy || !metadata.title.trim() || !metadata.url.trim() || !metadata.date.trim()}>
                {mutation.isPending ? "提交中…" : "确认导入"}
              </Button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
