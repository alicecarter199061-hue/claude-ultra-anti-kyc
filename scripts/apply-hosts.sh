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

# 刷新系统 DNS 缓存
flush_dns() {
    if [[ "$(uname)" == "Darwin" ]]; then
        echo "[*] 正在刷新 macOS DNS 缓存..."
        sudo dscacheutil -flushcache || true
        sudo killall -HUP mDNSResponder || true
    elif command -v resolvectl >/dev/null 2>&1; then
        echo "[*] 正在刷新 Linux DNS 缓存 (resolvectl)..."
        sudo resolvectl flush-caches || true
    elif command -v systemd-resolve >/dev/null 2>&1; then
        echo "[*] 正在刷新 Linux DNS 缓存 (systemd-resolve)..."
        sudo systemd-resolve --flush-caches || true
    elif command -v nscd >/dev/null 2>&1; then
        sudo nscd -i hosts || true
    fi
}

# 如果已存在旧标记段，先安全清理
if grep -qF "$MARKER_START" "$HOSTS_FILE"; then
    echo "[*] 检测到已存在旧的规则标记，正在清理替换..."
    sudo awk -v s="$MARKER_START" -v e="$MARKER_END" '
        $0 == s { skip=1; next }
        $0 == e { skip=0; next }
        !skip { print }
    ' "$HOSTS_FILE" | sudo tee "${HOSTS_FILE}.clean" >/dev/null
    sudo cp -p "${HOSTS_FILE}.clean" "$HOSTS_FILE"
    sudo rm -f "${HOSTS_FILE}.clean"
fi

# 安全写入新规则 (使用管道与 sudo tee，避免 bash -c 变量二次展开漏洞)
echo "[*] 写入最新黑洞规则..."
{
    printf '\n%s\n' "$MARKER_START"
    cat "$SOURCE_HOSTS"
    printf '\n%s\n' "$MARKER_END"
} | sudo tee -a "$HOSTS_FILE" >/dev/null

flush_dns

echo "[✓] 成功！Claude 域黑洞已生效 (IPv4 + IPv6 双栈已阻断)。"
echo "    本机所有普通程序访问 Claude 将被阻断在 127.0.0.1 / ::1。"
echo "    提示: 请确保您的专属浏览器与 Claude Code 通过专属 VPS 代理连接。"
