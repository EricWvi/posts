import type { PostStatus } from "@/lib/api";

export const statusLabel: Record<PostStatus, string> = {
  uploading: "上传中",
  ready: "待转换",
  queued: "排队中",
  converting: "转换中",
  done: "已完成",
  failed: "转换失败",
};

/** Whether the post is still on its way to done or failed. */
export const isPending = (status: PostStatus) => status === "uploading" || status === "queued" || status === "converting";

export function formatDateTime(iso: string): string {
  return new Date(iso).toLocaleString("zh-CN", { dateStyle: "medium", timeStyle: "short" });
}

export function hostOf(url: string): string {
  try {
    return new URL(url).host;
  } catch {
    return url;
  }
}

/** The day a post was published, as the page shows it, whether or not it also gave a time. */
export function publishedDay(publishedDate: string): string {
  return publishedDate.slice(0, 10);
}

/** A publish date as the page gave it: the day, plus the time and its offset when given. */
export function formatPublished(publishedDate: string): string {
  return publishedDate.replace("T", " ");
}

/** Toggle the current value; date-only values become midnight in the browser's timezone. */
export function togglePublishedDate(value: string): string | null {
  const date = value.trim();
  const day = date.slice(0, 10);
  if (!/^\d{4}-\d{2}-\d{2}$/.test(day)) return null;
  const utc = new Date(`${day}T00:00:00Z`);
  if (!Number.isFinite(utc.getTime()) || utc.toISOString().slice(0, 10) !== day) return null;

  if (date !== day) {
    if (!/^\d{4}-\d{2}-\d{2}T([01]\d|2[0-3]):[0-5]\d:[0-5]\d(?:\.\d+)?(?:Z|[+-]([01]\d|2[0-3]):[0-5]\d)$/.test(date)) return null;
    return Number.isFinite(Date.parse(date)) ? day : null;
  }

  const local = new Date(`${day}T00:00:00`);
  const offset = -local.getTimezoneOffset();
  const pad = (part: number) => String(part).padStart(2, "0");
  const zone = `${offset >= 0 ? "+" : "-"}${pad(Math.floor(Math.abs(offset) / 60))}:${pad(Math.abs(offset) % 60)}`;
  return `${day}T00:00:00${zone}`;
}
