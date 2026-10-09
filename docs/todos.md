# Posts TODO

用户上传 SingleFile 导出的 html，posts 清洗掉 css/js，把内嵌资源落盘为相对路径文件，再通过 wf-posts 调用 `wf`，把 html 中的正文转成 md。只转格式，不改正文内容。

## 已确认的决策

- **仓库结构**：单个 Go module。`cmd/posts`（主服务）、`cmd/wf-posts`（转换桥接）、`internal/...`、`frontend/`。结构、Taskfile、Dockerfile 参照 `/home/eric/projects/homepage`。
- **posts**：原生 Go + SQLite（纯 Go 驱动，`CGO_ENABLED=0`），前端构建产物 embed 进二进制。配置参照 homepage 的 `config.example.yaml`，OIDC 逻辑（`internal/auth`）照搬 homepage。
- **容器挂载两个目录**：
  - 配置目录 `/app/config`：`config.yaml`、`posts.db`。
  - 数据目录 `/app/data`：文章 md 和资源。
- **md 只存磁盘，不进数据库**。数据库只存元数据：标题、发布日期、slug、相对路径、状态等。
- **文章目录**：`<data>/<year>/<month>/<post-id>/<en-slug-title>.md`，资源放在同级的 `assets/`。
  - year/month 取文章发布日期。
  - post-id 是数据库自增 id。
- **数据隔离**：文章按 OIDC 用户隔离，与 homepage 一致。
- **wf 接入**：在 workflow 仓库新增 `html2md` 工作流，模型 `gpt-6.1-sol`、推理强度 `medium`。wf-posts 以子进程方式调用 `wf html2md`。
- **转换输出**（结构化输出）：
  - `title`：文章标题。
  - `published_date`：`YYYY-MM-DD`。能从页面判断就用页面上的日期，否则用当天日期，由 wf 在 prompt 中告诉模型。
  - `slug`：英文 slug，只含 `[a-z0-9-]`，非英文标题由模型意译。
  - `markdown`：正文 md。
- **正文范围**：prompt 的核心是"只保留正文"，由模型判断哪些属于正文，不针对某个站点写死规则。正文内的文字不增删改。
- **wf-posts**：Go 实现，参照 usage-gauge 的 codex-usage：单二进制，当前用户的 systemd user service，带安装向导脚本。不进 posts 的 Docker 镜像。
  - 每次转换在 `/tmp/wf-posts/<随机名>/` 下进行。
  - 不鉴权，只监听宿主机上容器网络可达的地址（见下文"wf-posts 监听地址"）。
- **任务调度**：posts 持有转换队列。SQLite 记录转换状态，后台 worker 串行调用 wf-posts（同步 HTTP、长超时）。前端轮询状态，失败可手动重试。
- **html 清洗范围**：
  - 删除 `<style>`、`<script>`、`<noscript>`、`<link rel=stylesheet|preload|...>`、`style` 属性、`on*` 事件属性。
  - 只被 css 引用的资源（如 `data:font/*`、css `url(data:...)`）随 css 一起丢弃，不落盘。
  - 删除内联 `<svg>`。
  - **保留 `class`**：全 div 布局的页面要靠 class 判断结构，也有助于模型区分正文和导航。
  - **删除 `data-*`**：对判断结构帮助不大。删除前先确认资源已在 `src`/`poster` 等标准属性中，不依赖 `data-src`。
  - 清洗阶段不删除 nav、header、aside 等节点，交给模型按 prompt 判断正文范围。
  - 删除 SingleFile 标记为隐藏（class `sf-hidden`）的元素：保存时页面上就没显示，去掉 css 后反而会冒出来。
  - 保留 `<script type=application/ld+json>` 和具名 `<meta>`：是元数据不是 js，有助于模型判断标题和发布日期。
  - 声明式 shadow DOM（`<template shadowrootmode>`）展开为普通子节点。
- **长文章**：第一版不分段，整篇交给模型。失败就标记为失败，分段放到后续阶段。
- **内容校验**：程序校验 md 中的每段文本都能在 html 纯文本中找到（归一化空白和标点后），以此发现模型改写或编造的内容。被舍弃的导航区域不计入差异。不通过时标记警告并展示差异。
- **前端功能**：上传、文章列表、在线阅读 md、下载 zip、删除、失败重试。中文界面。

### wf-posts 监听地址

posts 运行在容器里，容器内的 `127.0.0.1` 不是宿主机，所以 wf-posts 不能只监听 `127.0.0.1`。

方案：wf-posts 监听 homelab 自定义 Docker 网络 `my-network`（`172.28.1.0/24`）在宿主机上的网关地址，即 `172.28.1.1:55680`（部署前用 `docker network inspect` 确认）。

- 这个地址只有宿主机本身和 `my-network` 里的容器能访问，不对外网和局域网暴露，所以不需要鉴权。
- posts 配置 `wf_posts.url: http://172.28.1.1:55680`。
- 宿主机防火墙（如 ufw）要放行来自该网段的 55680 端口。
- 如果 Docker 网络还没创建好，wf-posts 会绑定失败。依靠 systemd 的 `Restart=on-failure` 重试即可。

## 阶段 0：项目骨架

- [x] `go.mod`、`cmd/posts`、`cmd/wf-posts`、`internal/` 目录。
- [x] `frontend/`：vite + React 19 + shadcn + React Query + zustand，TypeScript。
- [x] `Taskfile.yml`：install / build / format / lint / test / run:server / run:web / run:wf-posts，命名参照 homepage。
- [x] `AGENTS.md`：记录常用命令。
- [x] `config.example.yaml`：在 homepage 配置基础上调整和新增以下字段：
  - `data_dir`：文章目录。
  - `database`：默认放在配置文件所在目录的 `posts.db`。
  - `wf_posts.url`、`wf_posts.timeout`、`max_upload_size`。
- [x] `.gitignore` / `.dockerignore`：忽略 `config.yaml`、`*.db`、`data/`、`frontend/dist`、样例 html。

## 阶段 1：html 清洗与资源提取（`internal/singlefile`，纯函数）

- [x] 解析 SingleFile 头部注释，取原始 `url`、`saved date`。
- [x] 按上面的清洗范围删除 css/js 相关节点和属性。
- [x] 提取 `data:` 资源：`img[src|srcset]`、`video[src|poster]`、`source[src|srcset]`、`audio`、`picture`、`a[href]` 等。
  - 文件名用内容 sha256 前缀加 mime 推断的扩展名，同内容去重。
  - html 中改写为相对路径 `assets/<name>`。
- [x] 输出清洗后的 html 和资源列表，不直接写盘，方便测试。
- [x] 把仓库根目录的样例 `Plugin Extensions – Plugins ｜ OpenAI Developers.html` 移到 fixture 目录，改成不含空格和全角字符的文件名（如 `openai-plugin-extensions.html`）。
- [x] 测试：用这个样例（必要时裁剪成小 fixture）覆盖 img/webp/svg/png、video poster、mp4 source、字体丢弃、内联 svg 删除、data-* 删除、class 保留。

## 阶段 2：wf `html2md` 工作流（workflow 仓库）

- [x] 在 `/home/eric/projects/workflow` 新增 `html2md` 工作流：`wf html2md <input.html> <output.json> [--today YYYY-MM-DD]`。
  - 新增 profile：`gpt-6.1-sol` / `medium`。
  - 工作目录为输入文件所在目录。只读沙箱，审批策略 `never`，`skipGitRepoCheck`。
  - html 直接嵌入 prompt，单轮完成。
  - 用结构化输出返回 `title`、`published_date`、`slug`、`markdown`，由 wf 写入 output.json。
- [x] prompt 要求：
  - 核心是只保留正文，正文之外的页面内容一律舍弃。prompt 只讲通用原则，不写针对具体站点的规则。
  - 正文文字不增删改，只转格式。表格、代码块、列表、标题层级要忠实转换。
  - 图片、视频保留原相对路径 `assets/...`。md 没有原生语法的元素（如 video）保留 html 标签。
  - 发布日期优先取页面上的日期，判断不出时使用 prompt 中给出的当天日期。
  - slug 用英文小写短横线，非英文标题意译成英文。
- [x] 输出校验：日期格式和 slug 字符集不合法就报错退出。
- [x] 更新 workflow README 和测试（模拟 SDK 事件）。
- [x] 用样例 html 做一次真实调用冒烟，确认只保留了正文，站点导航、侧栏目录等被舍弃。（约 1 分钟；需要 codex SDK ≥ 0.162.0，旧版服务端拒绝 gpt-6.1-sol）

## 阶段 3：wf-posts 桥接服务（`cmd/wf-posts`）

- [x] `POST /api/convert`：请求体为清洗后的 html，返回 wf 输出的 JSON。
  - 每个请求创建 `/tmp/wf-posts/<随机名>/`，写入 `index.html`，执行 `wf html2md index.html result.json`。
  - 成功后删除该目录。失败时保留目录并在日志中打印路径，启动时清理 7 天以前的残留目录。
  - 串行执行，可配置超时。wf 失败返回 502，并附上 stderr 尾部；超时返回 504。错误响应为 `{"error":"..."}`。
- [x] 参数：`-listen`（默认 `172.28.1.1:55680`）、`-wf`（wf 可执行文件路径）、`-timeout`、`-workdir`（默认 `/tmp/wf-posts`）。不鉴权。另有 `GET /healthz`。
- [x] SIGINT/SIGTERM 时终止子进程并退出。
- [x] 测试：用假的 wf 脚本覆盖成功、失败、超时。

## 阶段 4：posts 后端

- [ ] `internal/config`：照搬 homepage 并扩展新增字段。`client_secret` 支持环境变量。
- [ ] `internal/auth`：照搬 homepage（OIDC、服务端 session、`dev_user`）。
- [ ] `internal/store`：SQLite 迁移。
  - `users`、`sessions`（同 homepage）。
  - `posts`：id、user_id、title、published_date、slug、path（相对 data_dir 的 md 路径）、source_url、uploaded_at、status（`queued` / `converting` / `done` / `failed`）、error、content_warning、converted_at。
- [ ] 文件处理：
  - 上传后先放到工作目录 `<data>/.work/<id>/`，内含 `index.html`（清洗后）和 `assets/`。
  - 转换成功后创建 `<data>/<year>/<month>/<id>/`，写入 `<slug>.md`，并只复制 md 实际引用到的资源到 `assets/`（导航图标等被舍弃的资源不保留）。随后删除工作目录。
  - 转换失败时保留工作目录，供重试。
- [ ] 上传 `POST /api/posts`（multipart，支持多文件，有大小上限）：清洗、写入工作目录、入队。
- [ ] 转换 worker：
  - 串行处理 `queued` 的文章，调用 wf-posts，按上面的规则落盘并更新元数据。
  - 转换后做内容校验，不通过就记录警告和差异摘要。
  - 启动时把残留的 `converting` 重置为 `queued`。
- [ ] API：
  - `GET /api/posts`：列表。
  - `GET /api/posts/{id}`：详情、状态、警告。
  - `GET /api/posts/{id}/md`：md 正文。
  - `GET /api/posts/{id}/assets/{name}`：资源文件。
  - `GET /api/posts/{id}/zip`：打包 md 和 `assets/`。
  - `POST /api/posts/{id}/retry`：重新入队。
  - `DELETE /api/posts/{id}`：删除记录、文章目录和工作目录。
- [ ] 所有 `/api` 都按当前用户过滤。
- [ ] 测试：store 迁移、上传→入队→假 wf-posts→完成/失败、目录布局、资源裁剪、用户隔离、内容校验。

## 阶段 5：前端

- [ ] 登录态处理和退出登录（参照 homepage 前端）。
- [ ] 上传：拖拽或选择多个 `.html`，用 zustand 管理上传进度。
- [ ] 文章列表：标题、发布日期、来源、状态徽标。有未完成任务时用 React Query 轮询。
- [ ] 详情页：
  - 用 react-markdown + remark-gfm 渲染 md，允许经过 sanitize 的 html（video 等）。
  - 资源相对路径改写到 `/api/posts/{id}/assets/`。
  - 展示内容校验警告和差异。
- [ ] 操作：下载 zip、删除（需确认）、失败后重试。
- [ ] 空状态、错误状态、移动端布局，跟随系统亮暗主题。

## 阶段 6：部署

- [ ] Dockerfile：多阶段构建（node 构建前端，go 编译），distroless nonroot 镜像，参照 homepage。只在 CI 构建，不在本地构建镜像。
- [ ] `compose.yaml`（在 root 用户下运行）：
  - 挂载配置目录 `/app/config` 和数据目录 `/app/data`。
  - 加入 `my-network`，通过 Caddy + Authelia 对外提供服务。
- [ ] wf-posts systemd user service：`deploy/wf-posts.service.in` 和 `scripts/install-wf-posts-service.sh`，加上 `task run:setup-wf-posts`。
  - 向导询问 wf 路径、监听地址（默认 `172.28.1.1:55680`）、代理。
  - 注入 `PATH`（wf 依赖 node/nvm）、`HOME`、代理变量。
  - 不启用 `PrivateTmp`，便于直接在 `/tmp/wf-posts` 排查失败任务。
  - 提示执行 `loginctl enable-linger`。
- [ ] README：部署步骤、OIDC 回调地址注册、Caddy/frpc 路由、wf-posts 监听地址和防火墙说明。

## 后续（第一版不做）

- [ ] 长文章分段转换后再拼接。
- [ ] 同一 URL 重复上传时提示或覆盖。
- [ ] 查看清洗后的 html。
- [ ] 全文搜索。
