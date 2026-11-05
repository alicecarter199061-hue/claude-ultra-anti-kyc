#!/usr/bin/env bash
# ==============================================================================
# revert-hosts.sh: 从 /etc/hosts 中完全移除 Claude 黑洞规则
# ==============================================================================
set -euo pipefail

HOSTS_FILE="/etc/hosts"
MARKER_START="# --- BEGIN CLAUDE BLACKHOLE SHIELD ---"
MARKER_END="# --- END CLAUDE BLACKHOLE SHIELD ---"

if ! grep -q "$MARKER_START" "$HOSTS_FILE"; then
    echo "[!] /etc/hosts 中未发现 Claude 黑洞标记，无需恢复。"
    exit 0
fi

echo "[*] 正在从 $HOSTS_FILE 移除 Claude 黑洞规则..."
BACKUP_PATH="/etc/hosts.revert.bak.$(date +%Y%m%d%H%M%S)"
sudo cp -p "$HOSTS_FILE" "$BACKUP_PATH"
echo "[+] 已创建移除前备份: $BACKUP_PATH"

sudo sed -i.tmp "/$MARKER_START/,/$MARKER_END/d" "$HOSTS_FILE"
sudo rm -f "${HOSTS_FILE}.tmp"

# 刷新本机 DNS 缓存
if [[ "$(uname)" == "Darwin" ]]; then
    sudo dscacheutil -flushcache || true
    sudo killall -HUP mDNSResponder || true
elif command -v systemd-resolve >/dev/null 2>&1; then
    sudo systemd-resolve --flush-caches || true
fi

echo "[✓] 已成功移除 Claude 黑洞规则，DNS 缓存已刷新。"
