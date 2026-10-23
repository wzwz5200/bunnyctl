#!/usr/bin/env bash
# 把 bunny-dsp 装到当前用户：二进制、udev 规则、systemd 用户服务。
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
BIN_NAME="bunny-dsp"
PREFIX="${PREFIX:-$HOME/.local}"
BIN_DIR="$PREFIX/bin"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UDEV_RULE="/etc/udev/rules.d/99-ktmicro-dsp.rules"
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
else
  info "安装 udev 规则（需要 sudo 密码）"
  sudo install -m 0644 "$ROOT/99-ktmicro-dsp.rules" "$UDEV_RULE"
  sudo udevadm control --reload-rules
  sudo udevadm trigger --subsystem-match=hidraw --action=change
fi

info "安装 systemd 用户服务"
mkdir -p "$UNIT_DIR"
sed -e "s|__BIN__|$BIN_DIR/$BIN_NAME|g" \
    -e "s|__PORT__|$PORT|g" \
    "$ROOT/bunny-dsp.service.tmpl" > "$UNIT_DIR/bunny-dsp.service"
systemctl --user daemon-reload

if [[ "${NO_START:-0}" != "1" ]]; then
  systemctl --user enable --now bunny-dsp.service
  info "服务已启动：http://127.0.0.1:$PORT"
else
  info "已安装（未启动）。手动启动：systemctl --user start bunny-dsp"
fi

echo
echo "  状态   systemctl --user status bunny-dsp"
echo "  日志   journalctl --user -u bunny-dsp -f"
echo "  卸载   ./uninstall.sh"
