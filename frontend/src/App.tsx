import { useQuery } from "@tanstack/react-query";
import { LogOut } from "lucide-react";
import { useState } from "react";
import { toast } from "sonner";
import { Link, Route, Switch } from "wouter";

import { UploadZone } from "@/components/upload-zone";
import { Button } from "@/components/ui/button";
import { api, goToLogin, logout } from "@/lib/api";
import { PostList } from "@/pages/post-list";
import { PostPage } from "@/pages/post-page";

export function App() {
  const [signedOut, setSignedOut] = useState(false);
  const me = useQuery({ queryKey: ["me"], queryFn: api.me, staleTime: Infinity, enabled: !signedOut });

  if (signedOut) {
    return (
      <main className="grid min-h-svh place-items-center p-4 text-center">
        <div className="space-y-4">
          <p>已退出登录</p>
          <Button onClick={goToLogin}>登录</Button>
        </div>
      </main>
    );
  }

  const signOut = async () => {
    try {
      await logout();
      setSignedOut(true);
    } catch (err) {
      toast.error(err instanceof Error ? err.message : String(err));
    }
  };

  return (
    <div className="mx-auto min-h-svh max-w-4xl px-4 pb-16 sm:px-6">
      <header className="flex items-center gap-3 py-5">
        <Link href="/" className="mr-auto text-lg font-semibold tracking-tight">
          Posts
        </Link>
        <Switch>
          <Route path="/">
            <UploadZone />
          </Route>
        </Switch>
        {me.data && (
          <Button variant="ghost" size="icon" onClick={signOut} title={`退出登录（${me.data.name || me.data.email}）`} aria-label="退出登录">
            <LogOut />
          </Button>
        )}
      </header>
      <main>
        <Switch>
          <Route path="/" component={PostList} />
          <Route path="/posts/:id">{(params) => <PostPage key={params.id} id={Number(params.id)} />}</Route>
          <Route>
            <p className="py-16 text-center text-sm text-muted-foreground">页面不存在</p>
          </Route>
        </Switch>
      </main>
    </div>
  );
}
