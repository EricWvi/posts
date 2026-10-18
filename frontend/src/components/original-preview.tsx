import { ArrowLeft, Ellipsis, Loader, Play } from "lucide-react";
import { Popover } from "radix-ui";
import { useEffect, useRef, useState, type PointerEvent } from "react";
import { createPortal } from "react-dom";
import { toast } from "sonner";
import { useLocation } from "wouter";

import { Button } from "@/components/ui/button";
import { useStartConversion } from "@/hooks/use-posts";
import { originalUrl, type Post } from "@/lib/api";

const size = 52;
const margin = 16;
const clamp = (x: number, y: number) => ({
  x: Math.max(margin, Math.min(x, window.innerWidth - size - margin)),
  y: Math.max(margin, Math.min(y, window.innerHeight - size - margin)),
});

export function OriginalPreview({ post }: { post: Post }) {
  const [, navigate] = useLocation();
  const start = useStartConversion();
  const [open, setOpen] = useState(false);
  const [dragging, setDragging] = useState(false);
  const [position, setPosition] = useState(() => clamp(window.innerWidth - size - 24, window.innerHeight - size - 24));
  const drag = useRef<{ x: number; y: number; left: number; top: number; moved: boolean } | null>(null);

  useEffect(() => {
    const previous = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const resize = () => setPosition((p) => clamp(p.x, p.y));
    window.addEventListener("resize", resize);
    return () => {
      document.body.style.overflow = previous;
      window.removeEventListener("resize", resize);
    };
  }, []);

  const move = (e: PointerEvent<HTMLButtonElement>) => {
    const d = drag.current;
    if (!d || !e.currentTarget.hasPointerCapture(e.pointerId)) return;
    if (Math.hypot(e.clientX - d.x, e.clientY - d.y) > 5) d.moved = true;
    if (d.moved) {
      setOpen(false);
      setPosition(clamp(d.left + e.clientX - d.x, d.top + e.clientY - d.y));
    }
  };

  return createPortal(
    <div className="fixed inset-0 z-40 bg-background">
      <iframe title={post.title} src={originalUrl(post.id)} sandbox="" referrerPolicy="no-referrer" className="h-full w-full border-0 bg-white" style={{ pointerEvents: dragging ? "none" : undefined }} />
      <div className="fixed z-50" style={{ left: position.x, top: position.y }}>
        <Popover.Root open={open} onOpenChange={setOpen}>
          <Popover.Trigger asChild>
            <button
              type="button"
              aria-label="网页菜单（可拖动）"
              aria-haspopup="dialog"
              aria-expanded={open}
              className="flex size-[52px] touch-none select-none items-center justify-center rounded-full border bg-card text-card-foreground shadow-lg hover:bg-accent focus-visible:outline-2 focus-visible:outline-ring active:cursor-grabbing"
              onPointerDown={(e) => {
                if (e.button !== 0) return;
                setDragging(true);
                drag.current = { x: e.clientX, y: e.clientY, left: position.x, top: position.y, moved: false };
                e.currentTarget.setPointerCapture(e.pointerId);
              }}
              onPointerMove={move}
              onPointerUp={() => setDragging(false)}
              onPointerCancel={() => { setDragging(false); drag.current = null; }}
              onClick={(e) => {
                if (drag.current?.moved) e.preventDefault();
                drag.current = null;
              }}
            >
              <Ellipsis aria-hidden />
            </button>
          </Popover.Trigger>
          <Popover.Portal>
            <Popover.Content
              aria-label="网页操作"
              side="top"
              align="end"
              sideOffset={10}
              collisionPadding={margin}
              className="z-50 w-44 rounded-xl border bg-popover p-1.5 text-popover-foreground shadow-lg"
            >
              <Button
                variant="ghost"
                className="w-full justify-start"
                disabled={start.isPending}
                onClick={() => start.mutate(post.id, {
                  onSuccess: () => navigate("/"),
                  onError: (err) => toast.error(err.message),
                })}
              >
                {start.isPending ? <Loader className="animate-spin" /> : <Play />}
                开始转换
              </Button>
              <Button variant="ghost" className="w-full justify-start" onClick={() => navigate("/")}>
                <ArrowLeft />返回列表
              </Button>
            </Popover.Content>
          </Popover.Portal>
        </Popover.Root>
      </div>
    </div>,
    document.body,
  );
}
