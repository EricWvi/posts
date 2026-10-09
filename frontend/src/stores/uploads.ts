import { create } from "zustand";

import { upload, type UploadResult } from "@/lib/api";

/** One batch of files sent in a single request. */
export type UploadBatch = {
  id: number;
  names: string[];
  /** 0–1 while sending. */
  progress: number;
  state: "sending" | "done" | "failed";
  /** Files the server rejected, or the request error. */
  errors: { file: string; error: string }[];
};

type UploadsState = {
  batches: UploadBatch[];
  /** Sends the files and resolves once the server has queued them. */
  send: (files: File[]) => Promise<UploadResult | null>;
  dismiss: (id: number) => void;
};

let nextId = 1;

export const useUploads = create<UploadsState>((set) => {
  const update = (id: number, patch: Partial<UploadBatch>) =>
    set((s) => ({ batches: s.batches.map((b) => (b.id === id ? { ...b, ...patch } : b)) }));

  return {
    batches: [],
    async send(files) {
      const id = nextId++;
      set((s) => ({
        batches: [...s.batches, { id, names: files.map((f) => f.name), progress: 0, state: "sending", errors: [] }],
      }));
      try {
        const result = await upload(files, (progress) => update(id, { progress }));
        if (result.errors.length === 0) {
          // Nothing to report: the posts show up in the list.
          set((s) => ({ batches: s.batches.filter((b) => b.id !== id) }));
        } else {
          update(id, { progress: 1, state: "done", errors: result.errors });
        }
        return result;
      } catch (err) {
        const error = err instanceof Error ? err.message : String(err);
        update(id, { state: "failed", errors: [{ file: files.map((f) => f.name).join("、"), error }] });
        return null;
      }
    },
    dismiss: (id) => set((s) => ({ batches: s.batches.filter((b) => b.id !== id) })),
  };
});
