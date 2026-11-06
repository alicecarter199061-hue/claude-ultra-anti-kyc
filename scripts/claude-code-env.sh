#!/usr/bin/env bash
# ==============================================================================
# claude-code-env.sh: Claude Code 安全运行环境与企业级代理包装器
# 作用:
# 1. 注入官方支持的企业代理环境变量 (HTTP_PROXY / HTTPS_PROXY / ALL_PROXY)
# 2. 彻底关闭所有遥测、打点与非核心网络通信 (CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1)
# 3. 强制覆盖系统时区 (TZ=Asia/Tokyo)，消除 Node.js 读取本地国内时区风控特征
# 4. 保持与专属网页端完全相同的单出口 IP 和 DNS
# ==============================================================================

# 配置出口代理地址 (优先读取 CLAUDE_PROXY_URL 或 PROXY_URL)
TARGET_PROXY="${CLAUDE_PROXY_URL:-${PROXY_URL:-""}}"
TARGET_TZ="${CLAUDE_TIMEZONE:-${PROXY_TIMEZONE:-"Asia/Tokyo"}}"

if [[ -z "$TARGET_PROXY" ]]; then
    echo "[-] 错误: 未指定出口代理地址！" >&2
    echo "    用法: CLAUDE_PROXY_URL=\"http://100.x.x.x:8080\" source $0" >&2
    echo "    或者: CLAUDE_PROXY_URL=\"http://100.x.x.x:8080\" $0 [claude 参数...]" >&2
    if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
        return 1 2>/dev/null || exit 1
    else
        exit 1
    fi
fi

# 同时设置大写与小写代理环境变量，防止特定底层 Node.js 库/子进程忽略大写变量
export HTTP_PROXY="$TARGET_PROXY"
export HTTPS_PROXY="$TARGET_PROXY"
export ALL_PROXY="$TARGET_PROXY"
export NO_PROXY="localhost,127.0.0.1"

export http_proxy="$TARGET_PROXY"
export https_proxy="$TARGET_PROXY"
export all_proxy="$TARGET_PROXY"
export no_proxy="localhost,127.0.0.1"

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

# 判断脚本是被 source 引入还是直接执行
if [[ "${BASH_SOURCE[0]}" != "${0}" ]]; then
    # 用户使用 source 引入，保留当前 Shell 的环境变量
    echo "[✓] 环境变量已成功注入当前 Shell。"
    echo "    您现在可以在此终端中直接执行 claude 命令。"
else
    # 用户直接运行脚本
    if command -v claude >/dev/null 2>&1; then
        exec claude "$@"
    else
        echo "[-] 错误: 未在系统 PATH 中找到 claude 二进制程序。" >&2
        echo "    如果您只是想在当前终端注入环境变量，请使用:" >&2
        echo "    source $0" >&2
        exit 1
    fi
fi
