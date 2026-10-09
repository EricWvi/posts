import { RotateCw } from "lucide-react";
import { toast } from "sonner";

import { Button } from "@/components/ui/button";
import { useRetry } from "@/hooks/use-posts";

export function RetryButton({ id, iconOnly }: { id: number; iconOnly?: boolean }) {
  const retry = useRetry();
  return (
    <Button
      variant={iconOnly ? "ghost" : "outline"}
      size={iconOnly ? "icon" : "default"}
      disabled={retry.isPending}
      aria-label="重试"
      title="重新转换"
      onClick={() => retry.mutate(id, { onError: (err) => toast.error(err.message) })}
    >
      <RotateCw />
      {!iconOnly && "重试"}
    </Button>
  );
}
