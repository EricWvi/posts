import { Pencil } from "lucide-react";
import { Dialog } from "radix-ui";
import { lazy, Suspense, useEffect, useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { useSaveMarkdown } from "@/hooks/use-posts";
import type { Post } from "@/lib/api";

const TextEditor = lazy(() => import("@/components/text-editor"));

export function MarkdownEdit({ post, markdown }: { post: Post; markdown: string }) {
  const [open, setOpen] = useState(false);
  const [initialValue, setInitialValue] = useState(markdown);
  const [draft, setDraft] = useState(markdown);
  const save = useSaveMarkdown();
  const dirty = draft !== initialValue;
  const editable = post.status === "done" && (post.palaceStatus === "" || post.palaceStatus === "failed");
  useEffect(() => {
    if (!open || !dirty) return;
    const prevent = (event: BeforeUnloadEvent) => event.preventDefault();
    window.addEventListener("beforeunload", prevent);
    return () => window.removeEventListener("beforeunload", prevent);
  }, [open, dirty]);
  if (!editable && !open) return null;
  return (
    <Dialog.Root open={open} onOpenChange={(next) => {
      if (save.isPending || (!next && dirty)) return;
      if (next) { setInitialValue(markdown); setDraft(markdown); save.reset(); }
      setOpen(next);
    }}>
      <Dialog.Trigger asChild>
        <Button variant="outline" size="sm"><Pencil />编辑 Markdown</Button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-black/40" />
        <Dialog.Content aria-describedby={undefined} className="fixed inset-3 z-50 flex flex-col gap-3 rounded-xl border bg-popover p-4 text-popover-foreground shadow-lg sm:inset-6 sm:p-6">
          <Dialog.Title className="text-lg font-semibold">编辑 Markdown</Dialog.Title>
          <p className="break-all text-xs text-muted-foreground">{post.path}</p>
          <Suspense fallback={<p className="flex-1 text-sm">加载编辑器…</p>}>
            <TextEditor initialValue={initialValue} onChange={setDraft} readOnly={save.isPending || !editable} />
          </Suspense>
          {save.error && <p role="alert" className="text-sm text-destructive">{save.error.message}</p>}
          <div className="flex items-center justify-end gap-2">
            <span className="mr-auto text-sm text-muted-foreground">{dirty ? "有未保存的修改" : "行号 · 自动换行"}</span>
            <Button variant="outline" disabled={save.isPending} onClick={() => setOpen(false)}>{dirty ? "放弃修改" : "取消"}</Button>
            <Button disabled={!dirty || save.isPending || !editable} onClick={() => save.mutate({ post, markdown: draft }, {
              onSuccess: () => { setOpen(false); toast.success("Markdown 已保存"); },
            })}>{save.isPending ? "保存中…" : "保存"}</Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}
