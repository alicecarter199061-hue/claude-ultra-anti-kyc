#!/usr/bin/env bash
# ==============================================================================
# self-check.sh: 本机 Claude 隔离防护自检工具
# 验证 hosts 黑洞拦截、出口 IP 纯净度、DNS 解析与时区配置
# ==============================================================================
set -euo pipefail

echo "========================================================"
echo "           Claude 独享单出口隔离防护方案自检"
echo "========================================================"

# 1. 检查 /etc/hosts 黑洞
echo -n "[1/4] 检查本地 hosts 黑洞拦截... "
RESOLVED_IP=$(python3 -c "import socket; print(socket.gethostbyname('claude.ai'))" 2>/dev/null || echo "failed")
if [[ "$RESOLVED_IP" == "127.0.0.1" ]]; then
    echo "✅ 成功 (claude.ai 已正确阻断至 127.0.0.1)"
else
    echo "❌ 失败 (当前解析为: $RESOLVED_IP，请先运行 sudo ./scripts/apply-hosts.sh)"
fi

# 2. 检查遥测域名阻断
echo -n "[2/4] 检查遥测域名阻断 (statsig.anthropic.com)... "
STATSIG_IP=$(python3 -c "import socket; print(socket.gethostbyname('statsig.anthropic.com'))" 2>/dev/null || echo "failed")
if [[ "$STATSIG_IP" == "127.0.0.1" ]]; then
    echo "✅ 成功 (已阻断)"
else
    echo "⚠️ 警告 (未阻断，解析为: $STATSIG_IP)"
fi

# 3. 检查系统时区特征
echo -n "[3/4] 检查当前 Shell 环境变量时区... "
CURRENT_TZ="${TZ:-$(date +%Z)}"
echo "当前时区: $CURRENT_TZ"
if [[ "$CURRENT_TZ" == *"CST"* || "$CURRENT_TZ" == *"Shanghai"* || "$CURRENT_TZ" == *"Hong_Kong"* ]]; then
    echo "    ⚠️ 提示: 本地时区存在中国大陆/香港特征。请务必使用 ./scripts/claude-code-env.sh 启动 Claude Code。"
else
    echo "    ✅ 当前时区符合海外规范。"
fi

# 4. 检查指定的代理连通性 (如果有设置)
PROXY_TARGET="${1:-${HTTP_PROXY:-""}}"
if [[ -n "$PROXY_TARGET" ]]; then
    echo -n "[4/4] 测试代理连通性 ($PROXY_TARGET)... "
    EXIT_IP=$(curl -s --connect-timeout 5 -x "$PROXY_TARGET" https://api.ipify.org 2>/dev/null || true)
    if [[ -n "$EXIT_IP" ]]; then
        echo "✅ 连通正常！代理实际出口 IP: $EXIT_IP"
    else
        echo "❌ 代理无法连接，请核对 Tailscale 或 VPS 代理服务是否正常运行。"
    fi
else
    echo "[4/4] 未指定代理地址，跳过出口网络连通测试。（用法: ./scripts/self-check.sh http://100.x.x.x:8080）"
fi

echo "========================================================"
