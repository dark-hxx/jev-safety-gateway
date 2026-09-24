#!/usr/bin/env bash
#
# JEV 安全网关 Linux 安装 / 升级脚本。
#
# 幂等：重复执行即为升级——停服务、换二进制、再起。
#
# 绝不触碰运行态与配置：
#   /var/lib/jev-safety-gateway/   数据库三件套（.db / -wal / -shm）
#   /etc/jev-safety-gateway/env    上游地址与密钥
# 这两个位置本脚本只创建缺失的部分，从不删除、从不覆盖已有内容。
# 卸载（--uninstall）同样保留它们，只在末尾提示手工清理命令。

set -euo pipefail

BIN_NAME=jev-safety-gateway
BIN_DIR=/usr/local/bin
UNIT_DIR=/etc/systemd/system
CONF_DIR=/etc/jev-safety-gateway
DOC_DIR=/usr/local/share/doc/${BIN_NAME}
STATE_DIR=/var/lib/${BIN_NAME}
SERVICE_USER=${BIN_NAME}

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"

die() { printf '错误：%s\n' "$1" >&2; exit 1; }
step() { printf '==> %s\n' "$1"; }
info() { printf '    %s\n' "$1"; }

[ "$(id -u)" -eq 0 ] || die '需要 root 权限运行（sudo ./install.sh）。'
command -v systemctl >/dev/null 2>&1 || die '未找到 systemctl：本脚本只支持 systemd 发行版。'

# ---------- 卸载 ----------
if [ "${1:-}" = "--uninstall" ]; then
    step '停止并禁用服务'
    systemctl stop "${BIN_NAME}.service" 2>/dev/null || true
    systemctl disable "${BIN_NAME}.service" 2>/dev/null || true

    step '移除 unit 与二进制'
    rm -f "${UNIT_DIR}/${BIN_NAME}.service"
    rm -f "${BIN_DIR}/${BIN_NAME}"
    systemctl daemon-reload

    cat <<EOF

卸载完成。以下内容被有意保留，请确认后再手工删除：

  数据库（全部配置与审计日志）：
    ${STATE_DIR}/
  初始化配置（含 JEV 密钥与管理员口令）：
    ${CONF_DIR}/

数据库是三件套 ${BIN_NAME}.db / ${BIN_NAME}.db-wal / ${BIN_NAME}.db-shm，
其中 -wal 可能远大于主库，单独留下主库没有意义——要么整体保留，要么整体删除。
删除前请先备份副本并确认副本可读：

    sudo systemctl stop ${BIN_NAME} 2>/dev/null || true
    sudo tar czf ~/jev-safety-gateway-backup-\$(date +%Y%m%d).tar.gz ${STATE_DIR} ${CONF_DIR}

服务用户 ${SERVICE_USER} 也保留着，需要时：userdel ${SERVICE_USER}
EOF
    exit 0
fi

# ---------- 定位待安装文件 ----------
SRC_BIN="${SCRIPT_DIR}/${BIN_NAME}"
[ -f "$SRC_BIN" ] || SRC_BIN="${SCRIPT_DIR}/../${BIN_NAME}"
[ -f "$SRC_BIN" ] || die "未找到二进制：请把本脚本与 ${BIN_NAME} 放在同一目录，或用 --binary 指定。"

SRC_UNIT="${SCRIPT_DIR}/${BIN_NAME}.service"
[ -f "$SRC_UNIT" ] || SRC_UNIT="${SCRIPT_DIR}/../systemd/${BIN_NAME}.service"
[ -f "$SRC_UNIT" ] || die "未找到 ${BIN_NAME}.service：它应与本脚本同目录（发布包内即是如此）。"

SRC_ENV="${SCRIPT_DIR}/env.example"
[ -f "$SRC_ENV" ] || SRC_ENV="${SCRIPT_DIR}/../systemd/env.example"

SRC_DOC="${SCRIPT_DIR}/deployment.md"
[ -f "$SRC_DOC" ] || SRC_DOC="${SCRIPT_DIR}/../deployment.md"

# ---------- 服务用户 ----------
if id -u "$SERVICE_USER" >/dev/null 2>&1; then
    info "服务用户 ${SERVICE_USER} 已存在，复用。"
else
    step "创建系统用户 ${SERVICE_USER}"
    useradd --system --no-create-home --shell /usr/sbin/nologin "$SERVICE_USER"
fi

# ---------- 二进制 ----------
step "安装二进制到 ${BIN_DIR}/${BIN_NAME}"
# 先停服务：正在运行的二进制在 Linux 上可以被覆盖，但换掉文件后运行中的进程
# 仍是旧映像，不重启就等于没升级。
if systemctl is-active --quiet "${BIN_NAME}.service"; then
    info '服务正在运行，先停止。'
    systemctl stop "${BIN_NAME}.service"
fi
install -m 0755 -o root -g root "$SRC_BIN" "${BIN_DIR}/${BIN_NAME}"

# ---------- unit ----------
step "安装 systemd unit 到 ${UNIT_DIR}/${BIN_NAME}.service"
install -m 0644 -o root -g root "$SRC_UNIT" "${UNIT_DIR}/${BIN_NAME}.service"

# ---------- 配置 ----------
if [ -d "$CONF_DIR" ]; then
    info "配置目录 ${CONF_DIR} 已存在，保留原有内容。"
else
    step "创建配置目录 ${CONF_DIR}"
    install -d -m 0755 -o root -g root "$CONF_DIR"
fi
if [ -f "${CONF_DIR}/env" ]; then
    info "${CONF_DIR}/env 已存在，未覆盖（升级不会重置上游地址与密钥）。"
elif [ -f "$SRC_ENV" ]; then
    install -m 0600 -o root -g root "$SRC_ENV" "${CONF_DIR}/env"
    info "已由 env.example 生成 ${CONF_DIR}/env，请填入上游地址与 JEV 密钥。"
else
    info "未找到 env.example，跳过；可在 ${CONF_DIR}/env 自行创建。"
fi

# ---------- 文档 ----------
if [ -f "$SRC_DOC" ]; then
    install -d -m 0755 -o root -g root "$DOC_DIR"
    install -m 0644 -o root -g root "$SRC_DOC" "${DOC_DIR}/deployment.md"
fi

# ---------- 启动 ----------
step '重新加载 systemd 并启动服务'
systemctl daemon-reload
systemctl enable --now "${BIN_NAME}.service"

sleep 1
if systemctl is-active --quiet "${BIN_NAME}.service"; then
    printf '\n安装完成，服务已启动。\n\n'
else
    printf '\n服务未能保持运行，请查看日志：\n  journalctl -u %s -n 50 --no-pager\n\n' "${BIN_NAME}" >&2
    exit 1
fi

cat <<EOF
  代理监听：:8080        （把客户端流量或 nginx 指到这里）
  管理控制台：127.0.0.1:8081  （只绑本机；远程访问请走 SSH 隧道或内网 nginx）

  下一步：
    1. 编辑 ${CONF_DIR}/env 填上游地址与 JEV 密钥，然后 systemctl restart ${BIN_NAME}
       也可留空，直接在控制台里配置
    2. 浏览器打开 http://127.0.0.1:8081 完成首次设置
       （远程机器用：ssh -L 8081:127.0.0.1:8081 <user>@<host>）

  常用命令：
    systemctl status ${BIN_NAME}
    journalctl -u ${BIN_NAME} -f
    systemctl restart ${BIN_NAME}

  数据库在 ${STATE_DIR}/，备份与升级注意事项见 ${DOC_DIR}/deployment.md
EOF
