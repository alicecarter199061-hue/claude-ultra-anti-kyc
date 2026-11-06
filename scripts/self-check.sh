#!/usr/bin/env bash
# ==============================================================================
# self-check.sh: 本机 Claude 隔离防护自检工具
# 验证 hosts 黑洞拦截、出口 IP 纯净度、DNS 解析与时区配置
# ==============================================================================
set -euo pipefail

echo "========================================================"
echo "           Claude 独享单出口隔离防护方案自检"
echo "========================================================"

# 1. 检查 /etc/hosts 黑洞 (IPv4 + IPv6 双栈)
echo -n "[1/4] 检查本地 hosts 黑洞拦截 (IPv4/IPv6)... "
RESOLVED_IPS=$(python3 -c "
import socket
try:
    infos = socket.getaddrinfo('claude.ai', 443)
    ips = set(item[4][0] for item in infos)
    print(','.join(sorted(ips)))
except Exception as e:
    print('failed')
" 2>/dev/null || echo "failed")

SAFE=true
for ip in $(echo "$RESOLVED_IPS" | tr ',' ' '); do
    if [[ "$ip" != "127.0.0.1" && "$ip" != "::1" && "$ip" != "failed" ]]; then
        SAFE=false
        break
    fi
done

if [[ "$SAFE" == "true" && "$RESOLVED_IPS" != "failed" && "$RESOLVED_IPS" != "" ]]; then
    echo "✅ 成功 (claude.ai 解析锁定为 $RESOLVED_IPS)"
else
    echo "❌ 失败 (解析存在公网泄露: $RESOLVED_IPS，请先运行 sudo ./scripts/apply-hosts.sh)"
fi

# 2. 检查遥测域名阻断
echo -n "[2/4] 检查遥测域名阻断 (statsig.anthropic.com)... "
STATSIG_IPS=$(python3 -c "
import socket
try:
    infos = socket.getaddrinfo('statsig.anthropic.com', 443)
    ips = set(item[4][0] for item in infos)
    print(','.join(sorted(ips)))
except Exception as e:
    print('failed')
" 2>/dev/null || echo "failed")

if [[ "$STATSIG_IPS" == *"127.0.0.1"* || "$STATSIG_IPS" == *"::1"* ]]; then
    echo "✅ 成功 (已阻断至 $STATSIG_IPS)"
else
    echo "⚠️ 警告 (未阻断，解析为: $STATSIG_IPS)"
fi

# 3. 检查系统时区特征 (精准匹配 IANA 与时间偏移，避免误判美国 CST)
echo -n "[3/4] 检查当前 Shell 环境变量与系统时区... "
TZ_NAME=$(python3 -c "
import os, time
from datetime import datetime
# 优先读环境变量 TZ，否则读系统真实时区
tz_env = os.environ.get('TZ')
if tz_env:
    print('ENV:' + tz_env)
else:
    # 读本地时间偏移
    offset_sec = -time.timezone if (time.localtime().tm_isdst == 0) else -time.altzone
    offset_hours = offset_sec // 3600
    print(f'SYSTEM:UTC{offset_hours:+d}')
" 2>/dev/null || echo "UNKNOWN")

echo "当前检测: $TZ_NAME"
IS_RISKY=false
if [[ "$TZ_NAME" == *"Shanghai"* || "$TZ_NAME" == *"Chongqing"* || "$TZ_NAME" == *"Urumqi"* || "$TZ_NAME" == *"Hong_Kong"* || "$TZ_NAME" == *"Taipei"* || "$TZ_NAME" == *"UTC+8"* ]]; then
    IS_RISKY=true
fi

if [[ "$IS_RISKY" == "true" ]]; then
    echo "    ⚠️ 提示: 本地时区存在中国大陆/香港特征。请务必使用 ./scripts/claude-code-env.sh 启动 Claude Code。"
else
    echo "    ✅ 当前时区符合海外规范。"
fi

# 4. 检查指定的代理连通性 (如果有设置)
PROXY_TARGET="${1:-${CLAUDE_PROXY_URL:-${PROXY_URL:-${HTTP_PROXY:-""}}}}"
if [[ -n "$PROXY_TARGET" ]]; then
    echo -n "[4/4] 测试代理连通性 ($PROXY_TARGET)... "
    EXIT_IP=$(curl -s --connect-timeout 5 --max-time 10 -x "$PROXY_TARGET" https://api.ipify.org 2>/dev/null || true)
    if [[ -n "$EXIT_IP" ]]; then
        echo "✅ 连通正常！代理实际出口 IP: $EXIT_IP"
    else
        echo "❌ 代理无法连接，请核对 Tailscale 或 VPS 代理服务是否正常运行。"
    fi
else
    echo "[4/4] 未指定代理地址，跳过出口网络连通测试。（用法: ./scripts/self-check.sh http://100.x.x.x:8080）"
fi

echo "========================================================"
