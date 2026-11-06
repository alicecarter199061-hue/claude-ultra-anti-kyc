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

sudo awk -v s="$MARKER_START" -v e="$MARKER_END" '
    $0 == s { skip=1; next }
    $0 == e { skip=0; next }
    !skip { print }
' "$HOSTS_FILE" | sudo tee "${HOSTS_FILE}.clean" >/dev/null
sudo cp -p "${HOSTS_FILE}.clean" "$HOSTS_FILE"
sudo rm -f "${HOSTS_FILE}.clean"

# 刷新本机 DNS 缓存
if [[ "$(uname)" == "Darwin" ]]; then
    sudo dscacheutil -flushcache || true
    sudo killall -HUP mDNSResponder || true
elif command -v resolvectl >/dev/null 2>&1; then
    sudo resolvectl flush-caches || true
elif command -v systemd-resolve >/dev/null 2>&1; then
    sudo systemd-resolve --flush-caches || true
elif command -v nscd >/dev/null 2>&1; then
    sudo nscd -i hosts || true
fi

echo "[✓] 已成功移除 Claude 黑洞规则，DNS 缓存已刷新。"
