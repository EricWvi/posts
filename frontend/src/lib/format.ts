import type { PostStatus } from "@/lib/api";

export const statusLabel: Record<PostStatus, string> = {
  uploading: "上传中",
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
