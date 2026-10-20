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
