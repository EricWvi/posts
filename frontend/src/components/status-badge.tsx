import { CircleAlert, CircleCheck, Loader } from "lucide-react";

import type { PostStatus } from "@/lib/api";
import { isPending, statusLabel } from "@/lib/format";
import { cn } from "@/lib/utils";

export function StatusBadge({ status, className }: { status: PostStatus; className?: string }) {
  const Icon = status === "done" ? CircleCheck : status === "failed" ? CircleAlert : Loader;
  return (
    <span
      className={cn(
        "inline-flex shrink-0 items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium",
        status === "done" && "bg-secondary text-muted-foreground",
        status === "failed" && "bg-destructive/10 text-destructive",
        isPending(status) && "bg-primary/10 text-foreground",
        className,
      )}
    >
      <Icon className={cn("size-3.5", isPending(status) && "animate-spin [animation-duration:2s]")} aria-hidden />
      {statusLabel[status]}
    </span>
  );
}
