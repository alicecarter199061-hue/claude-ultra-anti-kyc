# CPA 网关 Claude 账号接入与长效 Key 配置手册

本手册专门指导如何在 **CPA (CLIProxyAPI)** 网关上接入 Claude 官方账号，特别是如何配置**为期 1 年的持久长效 Key (Setup-Token)**，彻底解决由于 Refresh Token 轮换竞争导致的“莫名其妙掉号/反复登录”难题。

---

## 核心原则：同源出口网络锁死 (Same-Egress)

> ⚠️ **高危风控警告**：  
> Anthropic 对登录鉴权时的地理位置和 IP 纯净度极其敏感。  
> **严禁在本地中国大陆直连网络下打开网页点击授权！**  
> 进行以下任何一种授权操作时，您的指纹浏览器必须挂载 Sakura VPS 的代理（出口 IP 必须与 CPA 所在的 `49.212.144.166` 严格一致）。

---

## ⚡ 极速方案：在 Sakura VPS 服务端一键提取并配入（最推荐）

我们在 Sakura VPS 端已部署了专属交互式工具 `cpa-setup-token`，本地电脑**完全无需安装 Node.js 或 Claude CLI**，直接由 VPS 服务端完成同源签发与热加载。

### 操作步骤（仅需 2 步）：

1. **在 VPS 终端执行命令**：
   ```bash
   cpa-setup-token
   ```
   终端会当场打印出专属的 1 年长效 OAuth 授权 URL：
   ```text
   👉 步骤 1：请复制以下授权链接，在挂载了樱花 VPS 代理的指纹浏览器中打开：
   https://claude.ai/oauth/authorize?code=true&client_id=...
   ```

2. **指纹浏览器授权并回填**：
   - 复制该链接，在您挂载了樱花代理（出口为 `49.212.144.166`）的指纹浏览器中打开；
   - 登录您的 Claude 订阅账号并点击 **Authorize**；
   - 授权成功后，网页会展示一段授权码（格式类似 `xxxx#yyyy`）；
   - 将这段授权码复制，直接粘贴到 VPS 终端并回车！

🎉 **大功告成**：
- VPS 会以自身的日本原生 IP 直接向 Anthropic 换取 1 年长效令牌；
- 自动生成合规的 `/home/debian/cpa/auth/claude-<id>.json` 并将权限设为 `600`；
- CPA 网关 3 秒内自动热加载识别，管理面板直接上线，**整整一年免维护、零掉号风险**！

---

## 方案二：本地提取 1 年长效 Setup-Token 并注入 CPA

官方 Claude CLI (`@anthropic-ai/claude-code`) 提供了长效订阅 Token 机制，生成有效期长达 **365 天**的访问令牌（无需 Refresh Token，永不轮换失效）。

### 步骤 1：在同源出口网络下拉取长效 Token

1. 确保您的指纹浏览器或本地环境已通过 Sakura VPS 出口代理（验证 `https://api.ipify.org` 显示为 `49.212.144.166`）。
2. 在本地终端执行：
   ```bash
   CLAUDE_PROXY_URL="http://100.x.x.x:8080" claude setup-token
   ```
   *(注：若直接在本地运行，请确保开启了代理或者将终端给出的 URL 复制到指纹浏览器中完成确认)*。
3. 终端会提示：
   ```text
   This will guide you through long-lived (1-year) auth token setup for your Claude account.
   Visit the following URL to continue authentication:
   https://claude.ai/oauth/authorize?...
   ```
4. 将该 URL 复制到您的**樱花出口指纹浏览器**中打开，登录并点击 **Authorize**。
5. 授权完成后，页面会给出一个授权码或终端自动捕获，最终终端打印：
   ```text
   Your long-lived authentication token is:
   sk-ant-oat01-xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx
   ```
6. 复制该以 `sk-ant-oat01-` 开头的长效 Token。

### 步骤 2：使用项目注入脚本生成 CPA 凭据

我们在本项目中提供了全自动转换脚本：

```bash
# 运行注入脚本，粘贴刚才获得的 Token
./scripts/inject-claude-token-to-cpa.sh
```

脚本会自动：
- 格式化生成标准的 `claude-<uuid>.json` 文件；
- 自动将有效期时间戳设定为 1 年后（`expired: 2027-xx-xx`）；
- 将 `refresh_token` 留空（避免 CPA 触发破坏性的刷新动作）；
- 通过 SSH/SCP 自动上传到 Sakura VPS 的 `/home/debian/cpa/auth/` 目录；
- CPA 的文件热加载机制会在 3 秒内识别并装载该账号！

---

## 方案二：利用 CPA 原生 `-claude-login` 命令行配入

CPA 本身内置了官方 Claude CLI 的 OAuth 接收器。

### 步骤 1：本地建立 SSH 端口转发
因为无头 VPS 没有桌面浏览器，需要将 VPS 的回调端口映射到本地：

```bash
ssh -i ~/.ssh/sakura_49_212_144_166_ed25519 -L 54545:127.0.0.1:54545 debian@49.212.144.166
```

### 步骤 2：在 VPS 终端触发登录
在已连接的 VPS 终端中运行：

```bash
/home/debian/cpa/bin/cli-proxy-api -claude-login -no-browser -config /home/debian/cpa/config.yaml
```

终端会输出：
```text
Visit the following URL to continue authentication:
https://claude.ai/oauth/authorize?client_id=...&redirect_uri=http%3A%2F%2Flocalhost%3A54545%2Fcallback...
Waiting for Claude authentication callback...
```

### 步骤 3：指纹浏览器打开链接完成回调
1. 将终端给出的 `https://claude.ai/oauth/authorize?...` 完整链接，复制到您挂载了樱花代理的**指纹浏览器**中打开。
2. 登录您的 Claude 订阅账号并点击授权。
3. 浏览器会自动跳转至 `http://localhost:54545/callback?...`。
4. 由于第一步配置了 SSH `-L 54545:127.0.0.1:54545`，该请求会自动穿透到 VPS 上的 CPA 进程。
5. VPS 终端收到回调，提示 `Authentication successful!`，凭据自动写入 `/home/debian/cpa/auth/`。

---

## 方案三：利用内置 `claude-remote-login` Web 插件授权

VPS 上已加载并启用了 `claude-remote-login.so` 插件。

1. 在挂载了樱花代理的指纹浏览器中，直接访问：
   ```text
   http://49.212.144.166:28317/v0/resource/plugins/claude-remote-login/login
   ```
2. 页面会自动引导跳转至 Claude 官方登录页。
3. 登录并同意授权后，插件后台会自动捕获 Authorization Code 换取 Access Token，并直接在 VPS 端的 `/home/debian/cpa/auth/` 生成账号凭据。

---

## 验证与健康检查

无论采用上述哪种方式配入，配入完成后：

1. 打开管理控制台（指纹浏览器访问）：
   ```text
   http://49.212.144.166:28317/management.html
   ```
2. 输入您的管理密钥。
3. 在 **Accounts** 列表中可看到已上线的 Claude 账号，状态为 **Active / Healthy**。
4. 在本地发起一次测试请求：
   ```bash
   curl -s http://49.212.144.166:28317/v1/models \
     -H "Authorization: Bearer 48HdpHIDTDuVgTj9Vw2jLMwgSdDN1aai" | jq .
   ```
   返回列表中出现 `claude-3-5-sonnet-20241022`、`claude-opus-4-5-20251101` 即代表网关代理大功告成！
