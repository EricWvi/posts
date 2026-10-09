# Posts

把浏览器 SingleFile 插件保存的网页整理成 Markdown 存档。

1. 在页面上传 SingleFile 导出的 `.html`（可一次多个）。
2. posts 删除 css 和 js，把内嵌的图片、视频封面等资源存成文件，html 里改为相对路径 `assets/<hash>.<ext>`。
3. 后台按上传顺序逐篇调用 workflow 服务的 html2md API，由 `wf html2md`（converter profile，模型和推理强度在 workflow dashboard 配置）只保留正文转成 Markdown，并给出标题、发布日期和英文 slug。
4. 程序校验 Markdown 里的每段文字都能在原网页中找到，找不到的段落作为警告显示在文章页。
5. 文章存为 `<data>/<年>/<月>/<id>/<slug>.md`，只保留 Markdown 引用到的资源。可在线阅读、下载 zip、删除，失败后重试。

需求和分阶段计划见 [docs/todos.md](docs/todos.md)。

## 组成

| 组件 | 运行方式 | 说明 |
| --- | --- | --- |
| posts（`cmd/posts`） | root 的 docker compose | Go + SQLite（无 CGO），前端 embed 进二进制，OIDC 登录逻辑与 homepage 相同 |
| workflow（`/home/eric/projects/workflow`） | 当前用户的 systemd user service | `POST /api/v1/workflows/html2md` 收 `{"html": ...}`，执行 `wf html2md`，返回 JSON；串行、无鉴权 |

posts 在容器里，访问宿主机上的 workflow 服务要走 Docker 网络 `my-network` 在宿主机上的网关地址（默认 `172.28.1.1:55680`）。这个地址只有宿主机和该网络里的容器能访问，所以不做鉴权。workflow 的部署、profile 配置和接口文档见它的 README 和 dashboard。

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

### workflow（当前用户）

在 workflow 仓库执行 `task run:setup`，详见其 README。

### posts（root）

镜像由 GitHub Actions 在打 `v*` tag 时构建并推送到 GHCR（[release.yml](.github/workflows/release.yml)），不在本地构建。

```bash
mkdir -p config data && chown -R 65532:65532 config data   # distroless nonroot
cp config.example.yaml config/config.yaml                  # 修改 public_url、oidc、data_dir、workflow.url
docker compose up -d
```

对外访问照常配置 Caddy 路由（经 Authelia）和 frpc。

## 本地开发

```bash
task install:frontend
mkdir -p .data && cp config.example.yaml .data/config.yaml   # 设置 dev_user，workflow.url 改为 http://127.0.0.1:55680
# 终端 1：在 workflow 仓库执行 task run:server
task run:server                      # 终端 2，posts.db 和 data/ 都在 .data 下
task run:web                         # 终端 3，打开 http://localhost:5173
```

验证：`task test`（gofmt、go vet、前端类型检查和 Go 测试）。测试使用假的 wf 和转换器，不调用模型。
