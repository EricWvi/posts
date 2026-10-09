import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";

import { api, type Post } from "@/lib/api";
import { isPending } from "@/lib/format";

// Conversions take a minute or more; polling every few seconds is plenty.
const pollMs = 3000;

export function usePosts() {
  return useQuery({
    queryKey: ["posts"],
    queryFn: api.posts,
    refetchInterval: (q) => (q.state.data?.some((p) => isPending(p.status)) ? pollMs : false),
  });
}

export function usePost(id: number) {
  return useQuery({
    queryKey: ["posts", id],
    queryFn: () => api.post(id),
    refetchInterval: (q) => (q.state.data && isPending(q.state.data.status) ? pollMs : false),
  });
}

export function useMarkdown(post: Post | undefined) {
  return useQuery({
    queryKey: ["posts", post?.id, "md", post?.convertedAt],
    queryFn: () => api.markdown(post!.id),
    enabled: post?.status === "done",
    staleTime: Infinity,
  });
}

export function useRetry() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.retry,
    onSuccess: () => qc.invalidateQueries({ queryKey: ["posts"] }),
  });
}

export function useDelete() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: api.remove,
    onSuccess: (_, id) => {
      qc.removeQueries({ queryKey: ["posts", id] });
      return qc.invalidateQueries({ queryKey: ["posts"], exact: true });
    },
  });
}
