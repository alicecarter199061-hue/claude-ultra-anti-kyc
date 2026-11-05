#!/usr/bin/env bash
# ==============================================================================
# apply-hosts.sh: 将 Claude 黑洞规则写入系统 /etc/hosts
# 支持系统: macOS / Linux (安全备份，幂等写入)
# ==============================================================================
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOSTS_FILE="/etc/hosts"
SOURCE_HOSTS="${SCRIPT_DIR}/../hosts/claude-blacklist.hosts"
MARKER_START="# --- BEGIN CLAUDE BLACKHOLE SHIELD ---"
MARKER_END="# --- END CLAUDE BLACKHOLE SHIELD ---"

if [[ ! -f "$SOURCE_HOSTS" ]]; then
    echo "[-] 错误: 找不到黑洞规则文件: $SOURCE_HOSTS" >&2
    exit 1
fi

echo "[*] 正在准备应用 Claude 黑洞规则..."

# 备份当前的 /etc/hosts
BACKUP_PATH="/etc/hosts.bak.$(date +%Y%m%d%H%M%S)"
sudo cp -p "$HOSTS_FILE" "$BACKUP_PATH"
echo "[+] 已安全备份原 hosts 到: $BACKUP_PATH"

# 如果已存在旧标记段，先清理
if grep -q "$MARKER_START" "$HOSTS_FILE"; then
    echo "[*] 检测到已存在旧的规则标记，正在清理替换..."
    sudo sed -i.tmp "/$MARKER_START/,/$MARKER_END/d" "$HOSTS_FILE"
    sudo rm -f "${HOSTS_FILE}.tmp"
fi

# 写入新规则
echo "[*] 写入最新黑洞规则..."
sudo bash -c "cat <<EOF >> '$HOSTS_FILE'

$MARKER_START
$(cat "$SOURCE_HOSTS")
$MARKER_END
EOF"

# 刷新本机 DNS 缓存
if [[ "$(uname)" == "Darwin" ]]; then
    echo "[*] 正在刷新 macOS DNS 缓存..."
    sudo dscacheutil -flushcache || true
    sudo killall -HUP mDNSResponder || true
elif command -v systemd-resolve >/dev/null 2>&1; then
    sudo systemd-resolve --flush-caches || true
fi

echo "[✓] 成功！Claude 域黑洞已生效。本机所有普通程序访问 Claude 将被阻断在 127.0.0.1。"
echo "    提示: 请确保您的专属浏览器与 Claude Code 通过专属 VPS 代理连接。"
