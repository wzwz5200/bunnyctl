#!/usr/bin/env bash
set -euo pipefail

BIN_NAME="usagi"
PREFIX="${PREFIX:-$HOME/.local}"
UNIT_DIR="${XDG_CONFIG_HOME:-$HOME/.config}/systemd/user"
UDEV_RULE="/etc/udev/rules.d/99-usagi.rules"

info() { printf '\033[1;36m==>\033[0m %s\n' "$*"; }

if systemctl --user list-unit-files 2>/dev/null | grep -q '^usagi.service'; then
  info "停止并禁用服务"
  systemctl --user disable --now usagi.service || true
fi

info "删除 systemd 单元"
rm -f "$UNIT_DIR/usagi.service"
systemctl --user daemon-reload 2>/dev/null || true

info "删除二进制"
rm -f "$PREFIX/bin/$BIN_NAME"

if [[ -f "$UDEV_RULE" ]]; then
  read -r -p "删除 udev 规则 $UDEV_RULE ? [y/N] " ans
  if [[ "${ans,,}" == "y" ]]; then
    sudo rm -f "$UDEV_RULE"
    sudo udevadm control --reload-rules
  fi
fi

info "完成"
