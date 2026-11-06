#!/usr/bin/env bash
# ==============================================================================
# setup-vps-proxy.sh: 远端 VPS 极简代理一键搭建脚本 (Tailscale 内网安全绑定)
# 作用:
# 1. 在 VPS 上部署极简、轻量的代理服务 (Tinyproxy / Gost)
# 2. 严格限制仅监听在 Tailscale 私有内网网卡/IP 上，杜绝公网暴露被扫描探测
# 3. 仅服务于您本地的 Claude Code 和安全浏览器
# ==============================================================================
set -euo pipefail

PROXY_PORT="${1:-8080}"
if ! [[ "$PROXY_PORT" =~ ^[0-9]+$ ]] || [ "$PROXY_PORT" -lt 1 ] || [ "$PROXY_PORT" -gt 65535 ]; then
    echo "[-] 错误: 无效的端口号: $PROXY_PORT" >&2
    exit 1
fi

echo "=== 正在检查 Tailscale 状态 ==="
if ! command -v tailscale >/dev/null 2>&1; then
    echo "[-] 未检测到 Tailscale，正在自动安装 Tailscale..."
    curl -fsSL https://tailscale.com/install.sh | sh
    echo "[!] 请先运行 'sudo tailscale up' 将本服务器加入您的 Tailscale 局域网后再继续。"
    exit 1
fi

TS_IP=$(tailscale ip -4 2>/dev/null | head -n 1 || true)
if [[ -z "$TS_IP" ]]; then
    echo "[-] 错误: Tailscale 尚未连接，无法获取内网 IP。请先执行 'tailscale up'。" >&2
    exit 1
fi

echo "[+] 检测到 Tailscale 内网 IP: $TS_IP"
echo "[*] 正在安装轻量 HTTP 代理服务 (Tinyproxy)..."

sudo DEBIAN_FRONTEND=noninteractive apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq tinyproxy

CONFIG_FILE="/etc/tinyproxy/tinyproxy.conf"
sudo cp -p "$CONFIG_FILE" "${CONFIG_FILE}.bak.$(date +%Y%m%d%H%M%S)"

echo "[*] 正在配置 Tinyproxy 严格绑定 Tailscale IP: $TS_IP:$PROXY_PORT..."

# 注意:
# 1. Listen 用于限制入站监听，仅允许 Tailscale 虚拟内网接口访问。
# 2. 严禁配置 'Bind $TS_IP'！Tinyproxy 的 Bind 参数是用来指定向公网发起连接时的源 IP。
#    若将其绑定为内网 100.x.x.x，会导致发往公网的数据包无法路由，全部断网。
# 3. DisableViaHeader 开启可避免向 Claude 暴露 Tinyproxy 代理身份。
sudo bash -c "cat <<EOF > '$CONFIG_FILE'
User tinyproxy
Group tinyproxy
Port $PROXY_PORT
Listen $TS_IP
Timeout 600
DefaultErrorFile \"/usr/share/tinyproxy/default.html\"
StatFile \"/usr/share/tinyproxy/stats.html\"
LogLevel Info
MaxClients 100
MinSpareServers 5
MaxSpareServers 20
StartServers 10
MaxRequestsPerChild 0
DisableViaHeader Yes
# 仅允许 Tailscale 100.64.0.0/10 局域网段访问
Allow 100.64.0.0/10
Allow 127.0.0.1
ConnectPort 443
ConnectPort 563
EOF"

# 配置 systemd 服务依赖，防止 VPS 重启时 Tinyproxy 早于 Tailscale 启动导致找不到网卡崩溃
echo "[*] 配置 Tinyproxy 服务自启动依赖 (等待 Tailscale)..."
sudo mkdir -p /etc/systemd/system/tinyproxy.service.d
sudo bash -c "cat <<EOF > /etc/systemd/system/tinyproxy.service.d/tailscale.conf
[Unit]
After=network-online.target tailscaled.service
Wants=network-online.target tailscaled.service

[Service]
Restart=on-failure
RestartSec=5
EOF"

sudo systemctl daemon-reload
sudo systemctl restart tinyproxy
sudo systemctl enable tinyproxy

echo "======================================================================"
echo "[✓] VPS 端代理服务配置完成！"
echo "  • 监听地址: http://$TS_IP:$PROXY_PORT"
echo "  • 安全策略: 仅限 Tailscale 内部虚拟网访问，外网完全隔离"
echo ""
echo "在本机使用方式:"
echo "  1. 浏览器: ./scripts/launch-claude-browser.sh (设置 PROXY_URL=http://$TS_IP:$PROXY_PORT)"
echo "  2. 命令行: CLAUDE_PROXY_URL=http://$TS_IP:$PROXY_PORT ./scripts/claude-code-env.sh"
echo "======================================================================"
