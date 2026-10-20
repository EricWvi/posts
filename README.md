# Posts

把浏览器 SingleFile 插件保存的网页整理成 Markdown 存档。

1. 在页面上传 SingleFile 导出的 `.html`（可一次多个）。
2. 上传后先全屏预览原始 HTML。可拖动悬浮按钮，点击打开菜单，选择“开始转换”或“返回列表”；未开始转换的网页保留在列表中。
3. 点击“开始转换”后返回列表。后台清洗 HTML、提取内嵌资源，再逐篇调用 workflow 服务的 html2md API，由 `wf html2md`（converter profile，模型和推理强度在 workflow dashboard 配置）只保留正文转成 Markdown，并给出标题、发布日期和英文 slug。
4. 程序校验 Markdown 里的每段文字都能在原网页中找到，找不到的段落作为警告显示在文章页。
5. 列表按发布日期降序（转换前暂用上传日期），后台转换与等待状态不受标签页刷新影响。文章存为 `<data>/<年>/<月>/<id>/<slug>.md`，只保留 Markdown 引用到的资源。可在线阅读、下载 zip、删除，失败后重试。

业务规则和设计理由见 [docs/design.md](docs/design.md)。

## 组成

| 组件 | 运行方式 | 说明 |
| --- | --- | --- |
| posts（`cmd/posts`） | dotfiles `selfhost/only/docker-compose.yaml`（root） | Go + SQLite（无 CGO），前端 embed 进二进制，OIDC 登录逻辑与 homepage 相同 |
| workflow（`/home/eric/projects/workflow`） | 当前用户的 systemd user service | `POST /api/v1/workflows/html2md` 收 `{"html": ...}`，执行 `wf html2md`，返回 JSON；串行、无鉴权 |

posts 在容器里，访问宿主机上的 workflow 服务要走 Docker 网络 `my-network` 在宿主机上的网关地址（默认 `172.28.1.1:55680`）。这个地址只有宿主机和该网络里的容器能访问，所以不做鉴权。workflow 的部署、profile 配置和接口文档见它的 README 和 dashboard。

## 配置

见 [config.example.yaml](config.example.yaml)。容器挂载两个目录（宿主机上分别是 `/root/only/selfhost/posts/config` 和 `/home/eric/projects/reading-list`）：

```
config/            → /app/config
├─ config.yaml
└─ posts.db        # 自动创建，位置由 database 决定（相对路径以 config.yaml 所在目录为准）
reading-list/      → /app/data
├─ .work/<id>/     # 上传的原始 HTML 及待转换或失败的清洗结果
└─ 2026/10/12/plugin-extensions.md + assets/
```

容器内的 `config.yaml` 应设置 `data_dir: "/app/data"`。OIDC client secret 建议用环境变量 `POSTS_OIDC_CLIENT_SECRET`。在 Authelia 注册回调地址 `<public_url>/auth/callback`。

## 部署

### workflow（当前用户）

在 workflow 仓库执行 `task run:setup`，详见其 README。

### posts（root）

镜像由 GitHub Actions 在打 `v*` tag 时构建并推送到 GHCR（[release.yml](.github/workflows/release.yml)），不在本地构建。

最终镜像仍为 `distroless/static-debian12:nonroot`，内置 `/usr/local/bin/pal`。
CI 在构建前解析 Palace 最新正式 Release，将同一个 `PAL_VERSION` 传给 amd64、arm64 两个镜像；
下载对应的 musl 静态包并校验 SHA-256、ELF 架构及静态链接。旧的 GNU/glibc 包不兼容此安装流程，
须先发布包含两种 musl 附件的 pal Release。安装脚本与 Node 只在构建阶段，最终镜像不增加 shell 或包管理器。
CI 在两种原生架构上运行最终镜像中的 `pal --version` 与 `pal article upload --help`。

`Dockerfile` 的 `PAL_VERSION` 默认 `latest`；发布 CI 总是传入已解析的固定版本，避免构建缓存保留旧 CLI，
也避免两种架构在 latest 变化时安装不同版本。容器运行期间不自更新 pal。
本文只安装工具；posts 自动生成 `article.json`、调用上传命令和保存导入状态仍需单独接入。

compose 服务定义在 dotfiles 仓库的 `selfhost/only/docker-compose.yaml`（服务名 `posts`），`POSTS_OIDC_CLIENT_SECRET` 和其他 secrets 一样由 compose 环境变量提供。

文章直接写进 eric 的 `reading-list` 仓库，便于用 git 管理，所以容器以 `user: "1000:1000"`（eric）运行，config 目录也要归 eric 所有。`.work/` 是转换中的临时目录，应加入 reading-list 的 `.gitignore`。

```bash
mkdir -p /root/only/selfhost/posts/config
cp config.example.yaml /root/only/selfhost/posts/config/config.yaml   # 修改 public_url、oidc、data_dir、workflow.url
chown -R 1000:1000 /root/only/selfhost/posts/config
cd /root/dotfiles/selfhost/only && docker compose up -d posts
```

Caddy 把 `posts.onlyquant.top` 反代到 `posts:36751`；posts 自己做 OIDC 登录，不加 `forward_auth`。

## 本地开发

```bash
task install:frontend
mkdir -p .data && cp config.example.yaml .data/config.yaml   # 设置 dev_user，workflow.url 改为 http://127.0.0.1:55680
# 终端 1：在 workflow 仓库执行 task run:server
task run:server                      # 终端 2，posts.db 和 data/ 都在 .data 下
task run:web                         # 终端 3，打开 http://localhost:5173
```

验证：`task test`（gofmt、go vet、前端类型检查、Go 测试和 pal 安装脚本测试）。测试使用假的 wf 和转换器，不调用模型。
