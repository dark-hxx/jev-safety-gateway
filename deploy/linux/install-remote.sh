#!/usr/bin/env bash
# JEV 安全网关 · Linux 一键安装脚本
#
# 自动识别架构 → 从 GitHub Releases 拉取最新版对应的包 → 运行包内 install.sh
# 装成开机自启的 systemd 服务。再跑一次即为升级（包内 install.sh 幂等，不会
# 删除或覆盖数据库与 /etc/jev-safety-gateway/env）。
#
# 用法（需 root）：
#   curl -fsSL https://raw.githubusercontent.com/dark-hxx/jev-safety-gateway/master/deploy/linux/install-remote.sh | sudo bash
# 或克隆仓库后：
#   sudo bash deploy/linux/install-remote.sh
set -euo pipefail

REPO="dark-hxx/jev-safety-gateway"

if [ "$(id -u)" -ne 0 ]; then
  echo "需要 root 权限：请用 sudo 运行（如 curl ... | sudo bash）。" >&2
  exit 1
fi

case "$(uname -m)" in
  x86_64|amd64)  ARCH=amd64 ;;
  aarch64|arm64) ARCH=arm64 ;;
  *) echo "不支持的架构：$(uname -m)（预编译仅提供 amd64 / arm64，其它请走 Docker 或从源码编译）。" >&2; exit 1 ;;
esac

# 跟随 /releases/latest 的重定向解析出最新 tag，避免走 GitHub API 触发匿名限流。
latest_url="$(curl -fsSLI -o /dev/null -w '%{url_effective}' "https://github.com/${REPO}/releases/latest")"
ver="${latest_url##*/tag/v}"
if [ -z "$ver" ] || [ "$ver" = "$latest_url" ]; then
  echo "无法解析最新版本号（${latest_url}）——仓库可能还没有发布任何 Release。" >&2
  exit 1
fi

pkg="jev-safety-gateway-${ver}-linux-${ARCH}"
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT

echo "→ 下载 ${pkg}.tar.gz ..."
curl -fL -o "${tmp}/pkg.tar.gz" \
  "https://github.com/${REPO}/releases/download/v${ver}/${pkg}.tar.gz"
tar xzf "${tmp}/pkg.tar.gz" -C "$tmp"

echo "→ 安装 v${ver} ..."
cd "${tmp}/${pkg}"
exec bash install.sh
