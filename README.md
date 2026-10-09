# Posts

把浏览器 SingleFile 插件保存的网页整理成 Markdown 存档。

1. 在页面上传 SingleFile 导出的 `.html`（可一次多个）。
2. posts 删除 css 和 js，把内嵌的图片、视频封面等资源存成文件，html 里改为相对路径 `assets/<hash>.<ext>`。
3. 后台按上传顺序逐篇调用 wf-posts，由 `wf html2md`（gpt-6.1-sol medium）只保留正文转成 Markdown，并给出标题、发布日期和英文 slug。
4. 程序校验 Markdown 里的每段文字都能在原网页中找到，找不到的段落作为警告显示在文章页。
5. 文章存为 `<data>/<年>/<月>/<id>/<slug>.md`，只保留 Markdown 引用到的资源。可在线阅读、下载 zip、删除，失败后重试。

需求和分阶段计划见 [docs/todos.md](docs/todos.md)。

## 组成

| 组件 | 运行方式 | 说明 |
| --- | --- | --- |
| posts（`cmd/posts`） | root 的 docker compose | Go + SQLite（无 CGO），前端 embed 进二进制，OIDC 登录逻辑与 homepage 相同 |
| wf-posts（`cmd/wf-posts`） | 当前用户的 systemd user service | `POST /api/convert` 收 html，执行 `wf html2md`，返回 JSON；串行、无鉴权 |
| `wf html2md` | wf-posts 的子进程 | 在 `/home/eric/projects/workflow`，使用该用户的 Codex 登录 |

posts 在容器里，访问宿主机上的 wf-posts 要走 Docker 网络 `my-network` 在宿主机上的网关地址（默认 `172.28.1.1:55680`）。这个地址只有宿主机和该网络里的容器能访问，所以 wf-posts 不做鉴权。

## 配置

见 [config.example.yaml](config.example.yaml)。容器挂载两个目录：

```
config/            → /app/config
├─ config.yaml
└─ posts.db        # 自动创建，位置由 database 决定（相对路径以 config.yaml 所在目录为准）
data/              → /app/data
├─ .work/<id>/     # 待转换或转换失败的清洗后页面
└─ 2026/10/12/plugin-extensions.md + assets/
```

容器内的 `config.yaml` 应设置 `data_dir: "/app/data"`。OIDC client secret 建议用环境变量 `POSTS_OIDC_CLIENT_SECRET`。在 Authelia 注册回调地址 `<public_url>/auth/callback`。

## 部署

### wf-posts（当前用户）

前提：已 `npm link` 安装 wf，并用同一用户 `codex login`。

```bash
task run:setup-wf-posts
```

任务先构建 `build/wf-posts`，再由向导询问 wf 路径、监听地址（自动读取 `my-network` 网关，读不到时默认 `172.28.1.1:55680`）和代理，生成 `~/.config/systemd/user/wf-posts.service` 并启动。

- 开机后在登录前启动：root 执行一次 `loginctl enable-linger <用户名>`。
- Docker 网络还没创建时绑定会失败，服务每 5 秒重试。
- 启用了 ufw 时放行容器网段：`ufw allow from 172.28.1.0/24 to any port 55680 proto tcp`。
- 每次转换在 `/tmp/wf-posts/job-*` 进行，成功后删除；失败的保留，启动时清理 7 天前的残留。

```bash
systemctl --user status wf-posts.service
journalctl --user -u wf-posts.service -f
```

### posts（root）

镜像由 GitHub Actions 在打 `v*` tag 时构建并推送到 GHCR（[release.yml](.github/workflows/release.yml)），不在本地构建。

```bash
mkdir -p config data && chown -R 65532:65532 config data   # distroless nonroot
cp config.example.yaml config/config.yaml                  # 修改 public_url、oidc、data_dir、wf_posts.url
docker compose up -d
```

对外访问照常配置 Caddy 路由（经 Authelia）和 frpc。

## 本地开发

```bash
task install:frontend
cp config.example.yaml config.yaml   # 设置 dev_user，wf_posts.url 改为 http://127.0.0.1:55680
task run:wf-posts                    # 终端 1
task run:server                      # 终端 2
task run:web                         # 终端 3，打开 http://localhost:5173
```

验证：`task test`（gofmt、go vet、前端类型检查和 Go 测试）。测试使用假的 wf 和转换器，不调用模型。
