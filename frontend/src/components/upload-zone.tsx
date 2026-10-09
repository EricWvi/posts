import { useQueryClient } from "@tanstack/react-query";
import { FileUp, X } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { cn } from "@/lib/utils";
import { useUploads } from "@/stores/uploads";

const isHtml = (f: File) => /\.html?$/i.test(f.name);

/** Upload button plus a drop target covering the whole window. */
export function UploadZone() {
  const qc = useQueryClient();
  const send = useUploads((s) => s.send);
  const input = useRef<HTMLInputElement>(null);
  const [dragging, setDragging] = useState(false);

  const submit = async (list: FileList | null) => {
    const files = Array.from(list ?? []);
    const html = files.filter(isHtml);
    if (html.length < files.length) toast.error("只支持 SingleFile 导出的 .html 文件");
    if (html.length === 0) return;
    const result = await send(html);
    if (result && result.posts.length > 0) {
      toast.success(`已上传 ${result.posts.length} 篇，正在排队转换`);
      await qc.invalidateQueries({ queryKey: ["posts"], exact: true });
    }
  };

  useEffect(() => {
    let depth = 0;
    const hasFiles = (e: DragEvent) => e.dataTransfer?.types.includes("Files") ?? false;
    const enter = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depth++;
      setDragging(true);
    };
    const leave = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      depth = Math.max(0, depth - 1);
      if (depth === 0) setDragging(false);
    };
    const over = (e: DragEvent) => {
      if (hasFiles(e)) e.preventDefault();
    };
    const drop = (e: DragEvent) => {
      if (!hasFiles(e)) return;
      e.preventDefault();
      depth = 0;
      setDragging(false);
      void submit(e.dataTransfer?.files ?? null);
    };
    window.addEventListener("dragenter", enter);
    window.addEventListener("dragleave", leave);
    window.addEventListener("dragover", over);
    window.addEventListener("drop", drop);
    return () => {
      window.removeEventListener("dragenter", enter);
      window.removeEventListener("dragleave", leave);
      window.removeEventListener("dragover", over);
      window.removeEventListener("drop", drop);
    };
  });

  return (
    <>
      <Button onClick={() => input.current?.click()}>
        <FileUp />
        上传网页
      </Button>
      <input
        ref={input}
        type="file"
        accept=".html,.htm,text/html"
        multiple
        hidden
        onChange={(e) => {
          void submit(e.currentTarget.files);
          e.currentTarget.value = "";
        }}
      />
      <div
        aria-hidden={!dragging}
        className={cn(
          "pointer-events-none fixed inset-0 z-50 grid place-items-center bg-background/80 backdrop-blur-sm transition-opacity",
          dragging ? "opacity-100" : "opacity-0",
        )}
      >
        <div className="rounded-xl border-2 border-dashed border-ring px-10 py-8 text-center">
          <FileUp className="mx-auto mb-3 size-8 text-muted-foreground" />
          <p className="font-medium">松开即可上传</p>
          <p className="mt-1 text-sm text-muted-foreground">支持一次拖入多个 SingleFile 导出的 .html 文件</p>
        </div>
      </div>
    </>
  );
}

/** Progress of uploads in flight and files the server rejected. */
export function UploadStatus() {
  const batches = useUploads((s) => s.batches);
  const dismiss = useUploads((s) => s.dismiss);
  if (batches.length === 0) return null;
  return (
    <div className="space-y-2">
      {batches.map((b) => (
        <div key={b.id} className="rounded-lg border bg-card px-4 py-3 text-sm">
          {b.state === "sending" ? (
            <>
              <div className="flex justify-between gap-4">
                <span className="truncate">正在上传 {b.names.length === 1 ? b.names[0] : `${b.names.length} 个文件`}</span>
                <span className="tabular-nums text-muted-foreground">{Math.round(b.progress * 100)}%</span>
              </div>
              <div className="mt-2 h-1 overflow-hidden rounded-full bg-secondary">
                <div className="h-full bg-primary transition-[width]" style={{ width: `${b.progress * 100}%` }} />
              </div>
            </>
          ) : (
            <div className="flex items-start gap-3">
              <ul className="min-w-0 flex-1 space-y-1 text-destructive">
                {b.errors.map((e, i) => (
                  <li key={i} className="break-words">
                    <span className="font-medium">{e.file}</span>：{e.error}
                  </li>
                ))}
              </ul>
              <button
                type="button"
                onClick={() => dismiss(b.id)}
                className="text-muted-foreground hover:text-foreground"
                aria-label="关闭"
              >
                <X className="size-4" />
              </button>
            </div>
          )}
        </div>
      ))}
    </div>
  );
}
