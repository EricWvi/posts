export type PostStatus = "uploading" | "queued" | "converting" | "done" | "failed";

/** Report of Markdown blocks that are not text on the original page. */
export type ContentWarning = {
  checked: number;
  unmatchedCount: number;
  /** The first unmatched blocks, shortened. */
  unmatched: string[];
};

export type Post = {
  id: number;
  status: PostStatus;
  /** The page's <title> until the conversion names the article. */
  title: string;
  sourceUrl: string;
  savedAt: string | null;
  /** YYYY-MM-DD, set by the conversion. */
  publishedDate: string;
  slug: string;
  /** Markdown file relative to the data directory. */
  path: string;
  /** Why the last conversion failed. */
  error: string;
  warning: ContentWarning | null;
  uploadedAt: string;
  convertedAt: string | null;
};

export type User = {
  name: string;
  email: string;
};

export type UploadResult = {
  posts: Post[];
  errors: { file: string; error: string }[];
};

export class ApiError extends Error {
  readonly status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

export const goToLogin = () => window.location.assign("/auth/login");

async function request<T>(method: string, path: string, as: "json" | "text" = "json"): Promise<T> {
  let res: Response;
  try {
    res = await fetch(path, { method, cache: "no-store" });
  } catch {
    throw new ApiError(0, "无法连接服务器");
  }
  if (res.status === 401) {
    goToLogin();
    throw new ApiError(401, "未登录");
  }
  if (!res.ok) {
    const data = (await res.json().catch(() => null)) as { error?: string } | null;
    throw new ApiError(res.status, data?.error ?? `请求失败（${res.status}）`);
  }
  if (res.status === 204) return undefined as T;
  return (as === "json" ? res.json() : res.text()) as Promise<T>;
}

export const api = {
  me: () => request<User>("GET", "/api/me"),
  posts: () => request<Post[]>("GET", "/api/posts"),
  post: (id: number) => request<Post>("GET", `/api/posts/${id}`),
  markdown: (id: number) => request<string>("GET", `/api/posts/${id}/md`, "text"),
  retry: (id: number) => request<Post>("POST", `/api/posts/${id}/retry`),
  remove: (id: number) => request<void>("DELETE", `/api/posts/${id}`),
};

export const assetUrl = (id: number, path: string) => `/api/posts/${id}/${path}`;
export const zipUrl = (id: number) => `/api/posts/${id}/zip`;

/** Uploads html files, reporting the fraction sent so far. */
export function upload(files: File[], onProgress: (fraction: number) => void): Promise<UploadResult> {
  const form = new FormData();
  for (const f of files) form.append("files", f, f.name);
  return new Promise((resolve, reject) => {
    const xhr = new XMLHttpRequest();
    xhr.open("POST", "/api/posts");
    xhr.responseType = "json";
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded / e.total);
    };
    xhr.onerror = () => reject(new ApiError(0, "无法连接服务器"));
    xhr.onload = () => {
      if (xhr.status === 401) {
        goToLogin();
        reject(new ApiError(401, "未登录"));
      } else if (xhr.status >= 200 && xhr.status < 300) {
        resolve(xhr.response as UploadResult);
      } else {
        const data = xhr.response as { error?: string } | null;
        reject(new ApiError(xhr.status, data?.error ?? `上传失败（${xhr.status}）`));
      }
    };
    xhr.send(form);
  });
}

/** Ends this browser's session on the server. */
export async function logout() {
  let res: Response;
  try {
    res = await fetch("/auth/logout", { method: "POST", cache: "no-store" });
  } catch {
    throw new ApiError(0, "无法连接服务器，退出登录失败");
  }
  if (!res.ok) throw new ApiError(res.status, "退出登录失败");
}
