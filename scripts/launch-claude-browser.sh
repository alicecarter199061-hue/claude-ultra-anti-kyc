#!/usr/bin/env bash
# ==============================================================================
# launch-claude-browser.sh: 启动深度隔离的 Claude 专属安全浏览器
# 针对原推文方案的关键优化:
# 1. 彻底解决 WebRTC 旁路泄漏真实局域网与公网 IP 的高危漏洞 (disable_non_proxied_udp)
# 2. 独立沙箱目录 (User Data Dir)，不读取主机现有任何 Cookie、扩展与缓存
# 3. 强制远端 DNS 解析，杜绝 DNS 查询被本地网络劫持或暴露
# 4. 强制时区与语言对齐 (默认 Asia/Tokyo 或 America/New_York)，抹平系统 locale
# ==============================================================================
set -euo pipefail

# 代理配置 (优先读取 CLAUDE_PROXY_URL 或 PROXY_URL)
# 推荐填 Tailscale 分配的内网 IP:端口，例如: http://100.x.x.x:8080 或 socks5://100.x.x.x:1080
PROXY_URL="${CLAUDE_PROXY_URL:-${PROXY_URL:-""}}"
PROXY_TIMEZONE="${CLAUDE_TIMEZONE:-${PROXY_TIMEZONE:-"Asia/Tokyo"}}"
PROXY_LANG="${CLAUDE_LANG:-${PROXY_LANG:-"en-US,en"}}"

if [[ -z "$PROXY_URL" ]]; then
    echo "[-] 错误: 未指定出口代理地址！" >&2
    echo "    请设置环境变量，例如: PROXY_URL=\"http://100.x.x.x:8080\" $0" >&2
    exit 1
fi

PROFILE_DIR="${HOME}/.claude-isolated-browser"
mkdir -p "$PROFILE_DIR"

# 从 PROXY_URL 提取主机名/IP，用于豁免 Chrome 本地 DNS 屏蔽规则
PROXY_HOST=$(echo "$PROXY_URL" | sed -E 's|^[a-zA-Z0-9]+://||' | cut -d':' -f1 | cut -d'/' -f1)

# 查找 Chrome 可执行程序路径
CHROME_BIN=""
if [[ "$(uname)" == "Darwin" ]]; then
    CANDIDATES=(
        "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
        "${HOME}/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
        "/Applications/Brave Browser.app/Contents/MacOS/Brave Browser"
        "/Applications/Chromium.app/Contents/MacOS/Chromium"
    )
    for c in "${CANDIDATES[@]}"; do
        if [[ -x "$c" ]]; then
            CHROME_BIN="$c"
            break
        fi
    done
elif [[ "$(uname)" == "Linux" ]]; then
    CANDIDATES=(
        "google-chrome"
        "google-chrome-stable"
        "chromium-browser"
        "chromium"
        "brave-browser"
    )
    for c in "${CANDIDATES[@]}"; do
        if command -v "$c" >/dev/null 2>&1; then
            CHROME_BIN="$(command -v "$c")"
            break
        fi
    done
fi

if [[ -z "$CHROME_BIN" ]]; then
    echo "[-] 错误: 未找到可用的 Chrome / Chromium 浏览器。" >&2
    exit 1
fi

echo "[*] 启动 Claude 隔离浏览器..."
echo "    浏览器二进制: $CHROME_BIN"
echo "    出口代理地址: $PROXY_URL"
echo "    模拟注入时区: $PROXY_TIMEZONE"
echo "    沙箱配置目录: $PROFILE_DIR"
echo "    防泄漏防御项: WebRTC UDP 旁路拦截已激活、远程 DNS 解析已强制开启"

# 通过注入 TZ 环境变量与 Chromium 深度防风控参数启动
export TZ="$PROXY_TIMEZONE"

# 针对 macOS: Chrome 会忽略命令行 --lang，需额外通过 AppleLanguages 覆盖系统语言
EXTRA_MACOS_ARGS=()
if [[ "$(uname)" == "Darwin" ]]; then
    EXTRA_MACOS_ARGS=("-AppleLanguages" "(en-US)")
fi

# 防泄漏核心参数说明:
# 1. --webrtc-ip-handling-policy=disable_non_proxied_udp: 严格禁止 WebRTC 通过非代理的直接 UDP 连接
# 2. --force-webrtc-ip-handling-policy: 强制锁定 WebRTC 策略，阻止扩展或脚本绕过
# 3. 绝不使用 --disable-features=WebRtcHideLocalIpsWithMdns，保持 Chromium 默认的 mDNS 局域网 IP 遮盖
# 4. --host-resolver-rules: 强制本地 DNS 解析失败（MAP * ~NOTFOUND），除代理服务器本身外禁止一切本地 DNS 查询
exec "$CHROME_BIN" \
    --user-data-dir="$PROFILE_DIR" \
    --proxy-server="$PROXY_URL" \
    --host-resolver-rules="MAP * ~NOTFOUND , EXCLUDE ${PROXY_HOST}" \
    --webrtc-ip-handling-policy=disable_non_proxied_udp \
    --force-webrtc-ip-handling-policy \
    --enforce-webrtc-ip-permission-check \
    --dns-prefetch-disable \
    --disable-background-networking \
    --lang="$PROXY_LANG" \
    --no-first-run \
    --no-default-browser-check \
    "${EXTRA_MACOS_ARGS[@]}" \
    "https://claude.ai" \
    >/dev/null 2>&1 &

echo "[✓] 浏览器已在独立隔离沙箱中启动。"
