#!/usr/bin/env bash
# ==============================================================================
# inject-claude-token-to-cpa.sh
# 将 Claude 1年长效 Token (setup-token) 格式化并注入 CPA 网关 auth/ 目录
# 支持全自动生成合规 JSON、锁定 1 年有效期，并一键推送到 Sakura VPS
# ==============================================================================
set -euo pipefail

SSH_HOST="${CPA_SSH_HOST:-"sakura-49-212-144-166"}"
SSH_USER="${CPA_SSH_USER:-"debian"}"
VPS_IP="${CPA_VPS_IP:-"49.212.144.166"}"
REMOTE_AUTH_DIR="${CPA_AUTH_DIR:-"/home/debian/cpa/auth"}"
SSH_KEY="${CPA_SSH_KEY:-"${HOME}/.ssh/sakura_49_212_144_166_ed25519"}"

echo "=========================================================="
echo "    Claude 1年长效 Token (Setup-Token) -> CPA 注入器"
echo "=========================================================="

INPUT_TOKEN="${1:-""}"

if [[ -z "$INPUT_TOKEN" ]]; then
    echo -n "请输入您获取的 Claude 长效 Token (以 sk-ant-oat01- 开头): "
    read -r INPUT_TOKEN
fi

# 基础格式校验
if [[ ! "$INPUT_TOKEN" =~ ^sk-ant-oat01- ]]; then
    echo "[-] 警告: 输入的 Token 不是以 'sk-ant-oat01-' 开头。" >&2
    echo "    提示: 官方 1 年长效 setup-token 通常格式为: sk-ant-oat01-..." >&2
    echo -n "是否确认继续使用该 Token? (y/N): "
    read -r CONFIRM
    if [[ "$CONFIRM" != "y" && "$CONFIRM" != "Y" ]]; then
        echo "已取消。"
        exit 1
    fi
fi

echo -n "请输入该账号关联的 Email (可直接回车使用默认标识): "
read -r USER_EMAIL
if [[ -z "$USER_EMAIL" ]]; then
    USER_EMAIL="claude-user-$(date +%s)@isolated.local"
fi

# 生成唯一标识与 1 年有效期的 ISO-8601 时间戳
ACCOUNT_UUID=$(python3 -c "import uuid; print(str(uuid.uuid4()))")
ORG_UUID=$(python3 -c "import uuid; print(str(uuid.uuid4()))")
DEVICE_ID=$(python3 -c "import os, binascii; print(binascii.hexlify(os.urandom(32)).decode())")

NOW_ISO=$(python3 -c "from datetime import datetime, timezone; print(datetime.now(timezone.utc).isoformat())")
EXPIRED_ISO=$(python3 -c "from datetime import datetime, timedelta, timezone; print((datetime.now(timezone.utc) + timedelta(days=365)).isoformat())")

FILENAME="claude-${ACCOUNT_UUID:0:8}-${USER_EMAIL//[^a-zA-Z0-9]/_}.json"
TEMP_FILE="/tmp/${FILENAME}"

# 生成合规 JSON
python3 -c "
import json
data = {
    'access_token': '$INPUT_TOKEN',
    'account_uuid': '$ACCOUNT_UUID',
    'claude_device_ids': ['$DEVICE_ID'],
    'disabled': False,
    'email': '$USER_EMAIL',
    'expired': '$EXPIRED_ISO',
    'id_token': '',
    'last_refresh': '$NOW_ISO',
    'organization_name': 'Claude LongLived',
    'organization_uuid': '$ORG_UUID',
    'priority': 5,
    'refresh_token': '',
    'type': 'claude'
}
with open('$TEMP_FILE', 'w', encoding='utf-8') as f:
    json.dump(data, f, ensure_ascii=False, indent=2)
"

echo "[✓] 已在本地构建 CPA 凭据文件: $TEMP_FILE"
echo "    • 账号标识: $USER_EMAIL"
echo "    • 有效期限: 365 天 (至 $EXPIRED_ISO)"
echo "    • 刷新模式: 永久免 Refresh Token 轮换"

# 检查是否可以自动上传至 Sakura VPS
echo ""
echo "[*] 正在准备同步至 VPS 网关 ($VPS_IP)..."

UPLOAD_SUCCESS=false

# 尝试通过 SSH 密钥直传
if [[ -f "$SSH_KEY" ]]; then
    echo "[*] 检测到专属 SSH 密钥，正在通过 SCP 注入凭据..."
    if scp -i "$SSH_KEY" -o StrictHostKeyChecking=no "$TEMP_FILE" "${SSH_USER}@${VPS_IP}:${REMOTE_AUTH_DIR}/${FILENAME}" 2>/dev/null; then
        ssh -i "$SSH_KEY" -o StrictHostKeyChecking=no "${SSH_USER}@${VPS_IP}" "chmod 600 ${REMOTE_AUTH_DIR}/${FILENAME}" 2>/dev/null || true
        UPLOAD_SUCCESS=true
    fi
fi

# 若直传未配置，尝试通过系统默认 ssh alias
if [[ "$UPLOAD_SUCCESS" == "false" ]]; then
    if scp -o StrictHostKeyChecking=no "$TEMP_FILE" "${SSH_HOST}:${REMOTE_AUTH_DIR}/${FILENAME}" 2>/dev/null; then
        ssh -o StrictHostKeyChecking=no "${SSH_HOST}" "chmod 600 ${REMOTE_AUTH_DIR}/${FILENAME}" 2>/dev/null || true
        UPLOAD_SUCCESS=true
    fi
fi

if [[ "$UPLOAD_SUCCESS" == "true" ]]; then
    echo "=========================================================="
    echo "🎉 注入成功！凭据已就绪于 VPS: ${REMOTE_AUTH_DIR}/${FILENAME}"
    echo "   CPA 网关的文件热加载器将在 3 秒内自动激活该账号。"
    echo "   请通过管理后台查看健康状态:"
    echo "   http://${VPS_IP}:28317/management.html"
    echo "=========================================================="
else
    echo "[!] 自动上传跳过。您可以通过以下命令手动将凭据推送到 VPS:"
    echo "    scp $TEMP_FILE debian@${VPS_IP}:${REMOTE_AUTH_DIR}/${FILENAME}"
    echo "    ssh debian@${VPS_IP} 'chmod 600 ${REMOTE_AUTH_DIR}/${FILENAME}'"
fi
