#!/usr/bin/env bash
# ==============================================================================
# claude-code-env.sh: Claude Code 安全运行环境与企业级代理包装器
# 作用:
# 1. 注入官方支持的企业代理环境变量 (HTTP_PROXY / HTTPS_PROXY / ALL_PROXY)
# 2. 彻底关闭所有遥测、打点与非核心网络通信 (CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1)
# 3. 强制覆盖系统时区 (TZ=Asia/Tokyo)，消除 Node.js 读取本地国内时区风控特征
# 4. 保持与专属网页端完全相同的单出口 IP 和 DNS
# ==============================================================================

# 配置出口代理地址 (默认指向 Tailscale VPS 内网代理或本地隧道)
TARGET_PROXY="${CLAUDE_PROXY_URL:-"http://127.0.0.1:7898"}"
TARGET_TZ="${CLAUDE_TIMEZONE:-"Asia/Tokyo"}"

export HTTP_PROXY="$TARGET_PROXY"
export HTTPS_PROXY="$TARGET_PROXY"
export ALL_PROXY="$TARGET_PROXY"
export NO_PROXY="localhost,127.0.0.1"

# 官方企业网络开关：关闭非必要遥测、错误回传与分析打点
export CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1
export DISABLE_TELEMETRY=1
export DO_NOT_TRACK=1

# 屏蔽 Sentry 与追踪上报
export SENTRY_DSN=""

# 时区注入（解决 Node.js 读到国内本地时区的漏洞）
export TZ="$TARGET_TZ"

echo "[🛡️ Claude Code 隔离盾环境已加载]"
echo "  • 出口代理: $TARGET_PROXY"
echo "  • 注入时区: $TARGET_TZ"
echo "  • 遥测上报: 已完全静默禁用"

# 如果传入了参数，直接代为执行 claude 命令行
if command -v claude >/dev/null 2>&1; then
    exec claude "$@"
else
    echo "[!] 提示: 未在系统 PATH 中找到 claude 二进制。已完成当前 shell 的环境变量导出。"
    echo "    您可以在此终端中直接运行您的 Claude 客户端程序。"
fi
