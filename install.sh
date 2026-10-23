#!/usr/bin/env bash
# 把 bunnyctl 装到当前用户：二进制、udev 规则、systemd 用户服务。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_NAME="bunnyctl"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UDEV_RULE="/etc/udev/rules.d/99-bunnyctl.rules"
PORT="${PORT:-8099}"

info() { printf '\033[1;36m==>\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m!!\033[0m %s\n' "$*"; }

[[ "$(uname -s)" == "Linux" ]] || { warn "只支持 Linux（hidraw）"; exit 1; }
command -v go >/dev/null || { warn "需要 Go 1.22+：https://go.dev/dl/"; exit 1; }

info "编译 $BIN_NAME"
make -C "$ROOT" --no-print-directory build

info "安装二进制到 $BIN_DIR/$BIN_NAME"
mkdir -p "$BIN_DIR"
install -m 0755 "$ROOT/$BIN_NAME" "$BIN_DIR/$BIN_NAME"

if [[ -f "$UDEV_RULE" ]]; then
  info "udev 规则已存在，跳过"
elif sudo -n true 2>/dev/null || [[ -t 0 ]]; then
  info "安装 udev 规则（需要 sudo 密码）"
  sudo install -m 0644 "$ROOT/99-bunnyctl.rules" "$UDEV_RULE"
  sudo udevadm control --reload-rules
  sudo udevadm trigger --subsystem-match=hidraw --action=change
else
  warn "没有终端可以输入 sudo 密码，跳过 udev 规则。请手动执行："
  warn "  sudo install -m 0644 $ROOT/99-bunnyctl.rules $UDEV_RULE"
  warn "  sudo udevadm control --reload-rules && sudo udevadm trigger --subsystem-match=hidraw"
fi

if ! systemctl --user show-environment >/dev/null 2>&1; then
  warn "当前会话没有可用的 systemd --user，跳过开机自启"
  warn "直接运行即可：$BIN_DIR/$BIN_NAME"
else
  info "安装 systemd 用户服务"
  mkdir -p "$UNIT_DIR"
  sed -e "s|__BIN__|$BIN_DIR/$BIN_NAME|g" \
      -e "s|__PORT__|$PORT|g" \
      "$ROOT/bunnyctl.service.tmpl" > "$UNIT_DIR/bunnyctl.service"
  systemctl --user daemon-reload

  if [[ "${NO_START:-0}" != "1" ]]; then
    systemctl --user enable bunnyctl.service
    systemctl --user restart bunnyctl.service
    sleep 1
    if systemctl --user is-active --quiet bunnyctl.service; then
      info "服务已启动"
    else
      warn "服务没起来，看看日志：journalctl --user -u bunnyctl -n 30"
    fi
  else
    info "已安装（未启动）"
  fi
fi

echo
echo "  ────────────────────────────────────────────────"
printf '   访问地址   http://127.0.0.1:%s\n' "$PORT"
echo "  ────────────────────────────────────────────────"
printf '   状态       systemctl --user status bunnyctl\n'
printf '   日志       journalctl --user -u bunnyctl -f\n'
printf '   卸载       %s/uninstall.sh\n' "$ROOT"
if [[ "${NO_START:-0}" == "1" ]]; then
  printf '   启动       systemctl --user start bunnyctl\n'
fi
echo
