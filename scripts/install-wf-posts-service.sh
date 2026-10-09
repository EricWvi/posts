#!/usr/bin/env bash
# Installs wf-posts as a systemd user service of the current user, whose wf
# installation and Codex login it uses. Run through `task run:setup-wf-posts`,
# which builds build/wf-posts first.
set -euo pipefail

REPO_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
UNIT_TEMPLATE="$REPO_DIR/deploy/wf-posts.service.in"
UNIT_PATH="$HOME/.config/systemd/user/wf-posts.service"

if [[ -t 1 ]]; then BOLD=$'\e[1m'; DIM=$'\e[2m'; YELLOW=$'\e[33m'; GREEN=$'\e[32m'; RESET=$'\e[0m'
else BOLD=""; DIM=""; YELLOW=""; GREEN=""; RESET=""; fi
say()  { printf '  %s\n' "$1"; }
note() { printf '  %s%s%s\n' "$DIM" "$1" "$RESET"; }
warn() { printf '  %s⚠ %s%s\n' "$YELLOW" "$1" "$RESET"; }
die()  { warn "$1"; exit 1; }
# ask VAR "prompt" default — read a value, Enter keeps the default.
ask() {
  local input
  printf '  %s%s%s %s[%s]%s ' "$BOLD" "$2" "$RESET" "$DIM" "$3" "$RESET"
  read -r input || true
  printf -v "$1" '%s' "${input:-$3}"
}
escape_sed() { printf '%s' "$1" | sed 's/[\\\/&]/\\&/g'; }

[[ "$EUID" -ne 0 ]] || die "请用安装了 wf 并登录了 Codex 的普通用户运行，不要用 root。"
[[ -x "$REPO_DIR/build/wf-posts" ]] || die "未找到 $REPO_DIR/build/wf-posts，请先运行 task build:wf-posts。"
command -v systemctl >/dev/null || die "未找到 systemctl。"

# The gateway of the homelab network is where containers reach the host.
DEFAULT_LISTEN="172.28.1.1:55680"
if gateway=$(docker network inspect my-network -f '{{(index .IPAM.Config 0).Gateway}}' 2>/dev/null) && [[ -n "$gateway" ]]; then
  DEFAULT_LISTEN="$gateway:55680"
fi

printf '\n%s安装 wf-posts systemd user service%s\n\n' "$BOLD" "$RESET"
ask WF_BIN "wf 可执行文件：" "$(command -v wf || true)"
[[ -x "$WF_BIN" ]] || die "wf 不存在或不可执行：$WF_BIN"
ask LISTEN_ADDR "监听地址（Docker 网络 my-network 的网关）：" "$DEFAULT_LISTEN"
ask PROXY_URL "HTTP/HTTPS 代理：" "http://127.0.0.1:7890"

# wf is a Node script; nvm keeps node next to it.
NODE_DIR="$(dirname -- "$(command -v node || echo "$WF_BIN")")"
PATH_VALUE="$(dirname -- "$WF_BIN"):$NODE_DIR:/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

printf '\n'
note "项目目录：$REPO_DIR"
note "服务用户：$USER"
note "wf：$WF_BIN"
note "监听地址：$LISTEN_ADDR"
note "代理：$PROXY_URL"
printf '\n  %s? 现在安装并启动 wf-posts.service 吗？[y/N]%s ' "$YELLOW" "$RESET"
read -r reply || true
[[ "$reply" =~ ^[Yy] ]] || { warn "已取消，没有修改任何东西。"; exit 0; }

tmp="$(mktemp)"
trap 'rm -f "$tmp"' EXIT
sed \
  -e "s/@PROJECT_DIR@/$(escape_sed "$REPO_DIR")/g" \
  -e "s/@HOME_DIR@/$(escape_sed "$HOME")/g" \
  -e "s/@PATH@/$(escape_sed "$PATH_VALUE")/g" \
  -e "s/@PROXY_URL@/$(escape_sed "$PROXY_URL")/g" \
  -e "s/@LISTEN_ADDR@/$(escape_sed "$LISTEN_ADDR")/g" \
  -e "s/@WF_BIN@/$(escape_sed "$WF_BIN")/g" \
  "$UNIT_TEMPLATE" > "$tmp"
install -D -m 0644 "$tmp" "$UNIT_PATH"
systemctl --user daemon-reload
systemctl --user enable wf-posts.service
systemctl --user restart wf-posts.service

printf '\n  %s✓ wf-posts.service 已安装并启动%s\n' "$GREEN" "$RESET"
note "状态：systemctl --user status wf-posts.service"
note "日志：journalctl --user -u wf-posts.service -f"
note "开机后在登录前启动，需要 root 执行一次：loginctl enable-linger $USER"
note "若启用了 ufw，放行容器网段：ufw allow from 172.28.1.0/24 to any port ${LISTEN_ADDR##*:} proto tcp"
