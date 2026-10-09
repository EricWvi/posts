import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { StrictMode } from "react";
import { createRoot } from "react-dom/client";

import { App } from "@/App";
import { Toaster } from "@/components/ui/sonner";
import { ApiError } from "@/lib/api";

import "./index.css";

const queryClient = new QueryClient({
  defaultOptions: {
    queries: {
      // Not found and unauthenticated will not change on a retry.
      retry: (count, err) => !(err instanceof ApiError && err.status >= 400 && err.status < 500) && count < 2,
    },
  },
});

createRoot(document.getElementById("root")!).render(
  <StrictMode>
    <QueryClientProvider client={queryClient}>
      <App />
      <Toaster position="bottom-center" />
    </QueryClientProvider>
  </StrictMode>,
);
