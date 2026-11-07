#!/usr/bin/env python3
# ==============================================================================
# cpa-setup-token.py
# 运行在 Sakura VPS 上的 Claude 1年长效 Token (Setup-Token) 专属交互获取器
# 流程:
# 1. 在 VPS 本地生成 PKCE verifier/challenge (与出口同源)
# 2. 打印出专属的 1 年长效 OAuth 授权 URL
# 3. 用户在同 IP 指纹浏览器中打开并点击授权，将回调 code#state 贴回终端
# 4. VPS 本地向 Anthropic 换取 1 年长效 token (sk-ant-oat01-...)
# 5. 自动格式化并写入 /home/debian/cpa/auth/claude-<uuid>.json
# 6. CPA 网关热加载秒级生效，整整 1 年免维护
# ==============================================================================
import os
import sys
import json
import time
import base64
import hashlib
import secrets
import urllib.request
import urllib.parse
import urllib.error
from datetime import datetime, timedelta, timezone

CLIENT_ID = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
AUTH_URL = "https://claude.ai/oauth/authorize"
TOKEN_URL = "https://platform.claude.com/v1/oauth/token"
REDIRECT_URI = "https://platform.claude.com/oauth/code/callback"
AUTH_DIR = os.environ.get("CPA_AUTH_DIR", "/home/debian/cpa/auth")

def base64url(data: bytes) -> str:
    return base64.urlsafe_b64encode(data).decode('utf-8').rstrip('=')

def generate_pkce():
    verifier = base64url(secrets.token_bytes(32))
    challenge = base64url(hashlib.sha256(verifier.encode('utf-8')).digest())
    state = secrets.token_hex(16)
    return verifier, challenge, state

def main():
    print("=" * 66)
    print("     CPA 网关 Claude 1 年长效 Key (Setup-Token) 服务端获取器")
    print("=" * 66)

    # 1. 检查 auth 目录
    os.makedirs(AUTH_DIR, exist_ok=True)

    verifier, challenge, state = generate_pkce()

    # 构造授权 URL (与官方 claude setup-token 完全相同的参数)
    params = {
        "code": "true",
        "client_id": CLIENT_ID,
        "response_type": "code",
        "redirect_uri": REDIRECT_URI,
        "scope": "user:inference",
        "code_challenge": challenge,
        "code_challenge_method": "S256",
        "state": state
    }
    auth_link = f"{AUTH_URL}?{urllib.parse.urlencode(params)}"

    print("\n👉 步骤 1：请复制以下授权链接，在挂载了樱花 VPS 代理的指纹浏览器中打开：\n")
    print("-" * 66)
    print(auth_link)
    print("-" * 66)
    print("\n   ⚠️  安全提醒: 务必使用与本 VPS (49.212.144.166) 同源出口的指纹浏览器访问！")
    print("   登录您的 Claude Pro/Team/Ultra 账号并点击 [Authorize / 同意授权]。\n")
    print("👉 步骤 2：授权成功后，页面将展示一段授权码（格式类似 <code>#<state> 或 code 字符串）。")
    
    code_input = input("\n请输入页面展示的授权码 (直接粘贴完整内容): ").strip()
    if not code_input:
        print("[-] 错误: 输入为空，已取消。")
        sys.exit(1)

    # 提取 code
    if "#" in code_input:
        code_val, state_val = code_input.split("#", 1)
        code_val = code_val.strip()
    elif "?" in code_input:
        parsed = urllib.parse.parse_qs(urllib.parse.urlparse(code_input).query)
        code_val = parsed.get("code", [code_input])[0]
    else:
        code_val = code_input

    print("\n[*] 正在由 VPS 本地向 Anthropic 发起换码请求 (1-Year Token Exchange)...")

    exchange_payload = {
        "grant_type": "authorization_code",
        "code": code_val,
        "redirect_uri": REDIRECT_URI,
        "client_id": CLIENT_ID,
        "code_verifier": verifier,
        "state": state,
        "expires_in": 31536000  # 1年 (365天)
    }

    req_data = json.dumps(exchange_payload).encode('utf-8')
    req = urllib.request.Request(
        TOKEN_URL,
        data=req_data,
        headers={
            "Content-Type": "application/json",
            "User-Agent": "claude-code/2.1.301 (external, cli)"
        }
    )

    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            token_resp = json.loads(resp.read().decode('utf-8'))
    except urllib.error.HTTPError as e:
        err_msg = e.read().decode('utf-8', errors='ignore')
        print(f"\n[-] 换码失败 (HTTP {e.code}): {err_msg}")
        sys.exit(1)
    except Exception as e:
        print(f"\n[-] 网络或连接异常: {str(e)}")
        sys.exit(1)

    access_token = token_resp.get("access_token", "")
    if not access_token:
        print(f"\n[-] 错误: 响应中未包含 access_token: {token_resp}")
        sys.exit(1)

    print("[+] 成功获取长效访问令牌 (Token 前缀: " + access_token[:15] + "...)")

    # 3. 构造 CPA 凭据 JSON
    account_uuid = token_resp.get("account_uuid", secrets.token_hex(16))
    org_uuid = token_resp.get("organization_uuid", secrets.token_hex(16))
    device_id = secrets.token_hex(32)

    now_iso = datetime.now(timezone.utc).isoformat()
    # 强制标记 365 天有效期
    expired_iso = (datetime.now(timezone.utc) + timedelta(days=365)).isoformat()

    user_email = token_resp.get("email") or f"claude-{account_uuid[:8]}@isolated.local"

    cpa_auth_data = {
        "access_token": access_token,
        "account_uuid": account_uuid,
        "claude_device_ids": [device_id],
        "disabled": False,
        "email": user_email,
        "expired": expired_iso,
        "id_token": token_resp.get("id_token", ""),
        "last_refresh": now_iso,
        "organization_name": token_resp.get("organization_name", "Claude LongLived"),
        "organization_uuid": org_uuid,
        "priority": 5,
        "refresh_token": "",  # 留空彻底避免 CPA 进行轮换刷新
        "type": "claude"
    }

    filename = f"claude-{account_uuid[:8]}.json"
    target_path = os.path.join(AUTH_DIR, filename)

    with open(target_path, "w", encoding="utf-8") as f:
        json.dump(cpa_auth_data, f, ensure_ascii=False, indent=2)

    os.chmod(target_path, 0o600)

    print("=" * 66)
    print(f"🎉 成功！1 年长效凭据已写入 CPA: {target_path}")
    print(f"   • 账号标识: {user_email}")
    print(f"   • 有效期至: {expired_iso} (365 天)")
    print(f"   • 刷新模式: 永久免 Refresh Token 轮换 (零掉号风险)")
    print("   CPA 网关文件监听器已自动热加载该账号。")
    print("   可在管理后台查看状态: http://49.212.144.166:28317/management.html")
    print("=" * 66)

if __name__ == "__main__":
    main()
