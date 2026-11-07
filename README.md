# Claude Ultra 独享单出口隔离盾 (Claude Anti-KYC & Isolation Shield)

> 🛡️ **专为 Claude Ultra / Pro / Team 打造的“本机黑洞 + 纯净单出口”防风控与免 KYC 最佳实践方案。**  
> 灵感源自 Twitter [@Florious95](https://x.com/florious95/status/2118340225177124908) 的无 KYC 升档思路，并在 **WebRTC 穿透防泄漏**、**全量遥测拦截**、**时区指纹对齐** 与 **自动化工程脚本** 上进行了全方位深度优化。

---

## 📖 为什么升级 Claude Ultra 容易被 KYC / 封号？

社区普遍反馈：订阅升到 Claude Ultra 这一档基本必触发 Persona/Stripe KYC 身份审查，甚至本土正常用户频繁切换网络也会被拒。其核心原因**并不是单纯的 IP 归属地，而是复杂的环境多重暴露与指纹跳变**：

1. **多环境 IP 跳跃 (IP Churn)**：网页端、手机端、终端 CLI 在不同网络环境（公司 WiFi、手机热点、梯子不同节点）之间来回切换，几小时内出现不同 ASN 与机房特征。
2. **DNS 泄露与三方监控上报**：本地网络后台进程直连 Claude 或 Anthropic 的关联追踪域名（如 Statsig、Datadog、Sentry），DNS 请求直接被国内运营商记录。
3. **WebRTC 旁路穿透（推特方案致命盲区）**：仅使用普通的 `--proxy-server` 启动 Chrome，**WebRTC STUN 依然会直接绕过代理**，向外探测并上报本机的真实内网 IP 与物理公网 IP。
4. **系统时区漏洞**：Claude Code 等工具运行在本地 Node.js 运行时，会直接通过 `Intl.DateTimeFormat()` 读取宿主机系统时区（例如 `Asia/Shanghai`）。即便走了海外代理，请求正文中依然携带国内时区特征。

---

## 💡 核心设计架构

```text
┌────────────────────────────────────────────────────────────────────────┐
│                        本机客户端 (Local Client)                         │
│                                                                        │
│   [普通应用 / 背景程序]        [Claude Code 终端]       [隔离专属 Chrome]  │
│            │                          │                        │       │
│            ▼                          │                        │       │
│    /etc/hosts 黑洞拦截                 │                        │       │
│   (127.0.0.1 全部拒接)                 │                        │       │
│            ✗                          ▼                        ▼       │
│   ┌─────────────────┐       ┌──────────────────────────────────────┐   │
│   │ 杜绝任何意外直连 │       │  强制绑定 Tailscale 内网加密通道      │   │
│   └─────────────────┘       │  注入 TZ 时区伪装 & 禁用非必要遥测     │   │
│                             │  禁用 WebRTC 穿透 & 启用远程 DNS 解析  │   │
│                             └──────────────────┬───────────────────┘   │
└────────────────────────────────────────────────┼───────────────────────┘
                                                 │ 私有虚拟局域网 (Tailscale)
                                                 ▼
                              ┌─────────────────────────────────────┐
                              │       远端专属干净 VPS (Exit Node)    │
                              │  • Tinyproxy 仅监听 100.x 内网 IP    │
                              │  • 统一代为解析 DNS (无本地 DNS 泄露) │
                              │  • 进出所有 Anthropic 流量保持单一 IP │
                              └──────────────────┬──────────────────┘
                                                 │ 稳定单一出口
                                                 ▼
                                     [Anthropic / Claude API]
```

---

## ✨ 相对原方案的 5 项关键优化

| 维度 | 原推文粗糙做法 | 本项目深度优化改进 |
|---|---|---|
| **WebRTC 防泄漏** | 仅使用 Chrome `--proxy-server`，**WebRTC STUN 仍然直连泄露真实 IP** | 引入 `--webrtc-ip-handling-policy=disable_non_proxied_udp` 阻断非代理 UDP，同时完整保留 Chromium mDNS 局域网遮盖，杜绝泄露 |
| **黑洞拦截覆盖度** | 手动配几条域名，仅填 IPv4 | 整理 30+ 核心业务域、Artifacts 沙盒域及 Sentry/Statsig/Datadog 遥测域，**全面支持 IPv4 (127.0.0.1) 与 IPv6 (::1) 双栈拦截**，防 Happy Eyeballs 绕过 |
| **远端代理健壮性** | 原生配置容易在公网暴露或出站死锁 | Tinyproxy 严格仅在 Tailscale 内网网卡监听（`Listen $TS_IP`），剥离 `Via` 特征头，配置 systemd 依赖 `tailscaled.service` 自动重启 |
| **时区/Locale 防护** | 未做防护，Node.js 运行时直接暴露国内时区 | 启动器自动注入 `TZ=Asia/Tokyo`（与出口节点一致），macOS 浏览器追加 `-AppleLanguages` 强制英文，消除时区风控扣分 |
| **工程化体验** | 全程手动敲复杂命令与编辑 hosts | 提供自动化部署、安全备份还原、一键浏览器拉起、Shell 环境注入与双栈健康自检脚本 |

---

## 🚀 快速上手指南

### 步骤 1：远端 VPS 部署内网极简代理

1. 在一台干净的海外 VPS（推荐美国或日本纯净线路）上安装并登录 [Tailscale](https://tailscale.com/)：
   ```bash
   curl -fsSL https://tailscale.com/install.sh | sh
   sudo tailscale up
   ```
2. 下载并执行 VPS 端一键代理配置脚本：
   ```bash
   curl -fsSL https://raw.githubusercontent.com/alicecarter199061-hue/claude-ultra-anti-kyc/main/server/setup-vps-proxy.sh | bash
   ```
   *该脚本会自动配置 Tinyproxy 并**严格仅监听在 Tailscale 分配的内网 IP（100.x.x.x）上**，公网无法探测，无需担心被扫。*

---

### 步骤 2：本机配置 Claude 域名黑洞拦截

1. 克隆本项目：
   ```bash
   git clone https://github.com/alicecarter199061-hue/claude-ultra-anti-kyc.git
   cd claude-ultra-anti-kyc
   ```
2. 运行生效脚本（会自动备份原有 hosts，幂等写入）：
   ```bash
   sudo ./scripts/apply-hosts.sh
   ```
   *此时在终端 `ping claude.ai` 会显示 `127.0.0.1`，所有意外的本地直连已被物理切断。*

---

### 步骤 3：启动专属隔离浏览器访问 Claude Web 端

使用封装好的防泄漏启动器拉起 Chrome：

```bash
# 替换为您 VPS 的 Tailscale IP:端口
PROXY_URL="http://100.x.x.x:8080" ./scripts/launch-claude-browser.sh
```

- ✅ **独立用户沙箱**：运行在 `~/.claude-isolated-browser`，不触碰日常主浏览器的 Cookie 与插件。
- ✅ **WebRTC 禁用旁路**：UDP 探测全部受制于代理，不暴露本地 IP。
- ✅ **远程 DNS**：由远端代理 VPS 解析域名，完全绕过本机 hosts 拦截。

---

### 步骤 4：启动 Claude Code (CLI)

方式 A：直接拉起 Claude Code（如果已在 PATH 中安装）：
```bash
CLAUDE_PROXY_URL="http://100.x.x.x:8080" ./scripts/claude-code-env.sh
```

方式 B：向当前终端会话注入安全环境变量（推荐日常开发）：
```bash
CLAUDE_PROXY_URL="http://100.x.x.x:8080" source ./scripts/claude-code-env.sh
claude
```

脚本自动注入：
- 官方企业代理：同时导出大写及小写 `HTTP_PROXY` / `http_proxy`、`HTTPS_PROXY` / `https_proxy`、`ALL_PROXY` / `all_proxy`
- 遥测拦截：`CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC=1`、`DISABLE_TELEMETRY=1`
- 时区对齐：`TZ=Asia/Tokyo`（或对应 VPS 时区）

---

### 步骤 5：一键自检与健康排查

随时运行自检工具验证防护状态：

```bash
./scripts/self-check.sh http://100.x.x.x:8080
```

---

## ⚡ CPA 网关集成：配置 1 年长效 Key (Setup-Token)

对于使用 **CPA (CLIProxyAPI)** 作为多模型网关的用户，常规短期 OAuth Token（Refresh Token Rotation）极易因为网络刷新竞争或多实例冲突导致“频繁掉号”。

本项目完整支持通过两种最稳妥的方式接入 **1 年长效 Setup-Token**：

1. **升级版 Web 登录插件（首选）**：  
   直接在指纹浏览器访问：  
   `http://49.212.144.166:28317/v0/resource/plugins/claude-remote-login/login`  
   插件已升级为 v1.1.0，默认走 1 年长效 Setup-Token 换码，页面授权后全自动落盘热加载，**零命令行、零刷新轮换、整整一年不掉号**！

2. **服务端全局命令（备选）**：  
   在 VPS 终端直接执行 `cpa-setup-token`，指纹浏览器同源回填。

📘 深度架构与实操手册：
- [CPA 网关接入与长效 Key 配置手册 (包含 Web 插件实操)](docs/cpa-claude-auth-guide.md)
- [ADR 0001: Claude 1年长效 Token 接入 CPA 架构裁决](docs/adr/0001-claude-setup-token-cpa-integration.md)
- [领域术语表 (Domain Glossary)](GLOSSARY.md)

---

## 🛠️ 脚本清单

- `hosts/claude-blacklist.hosts`：全量 Claude / Anthropic 域拦截列表 (IPv4 + IPv6 双栈)。
- `scripts/apply-hosts.sh`：将黑洞规则安全合入 `/etc/hosts` 并刷新系统 DNS。
- `scripts/revert-hosts.sh`：一键无损还原 `/etc/hosts` 到修改前状态。
- `scripts/launch-claude-browser.sh`：防 WebRTC 泄露、隔离 Profile 的专用浏览器启动器。
- `scripts/claude-code-env.sh`：Claude Code 企业级代理与反指纹包装器。
- `scripts/self-check.sh`：本地环境、hosts 状态与出口 IP 连通性自检。
- `scripts/inject-claude-token-to-cpa.sh`：将 Claude 1 年长效 Token 转换为 CPA 凭据并一键部署。
- `server/setup-vps-proxy.sh`：远端 VPS Tailscale 极简代理一键配置。

---

## ❓ 常见问题 (FAQ)

#### Q1: hosts 把 claude.ai 拦了，浏览器和 Claude Code 怎么还能连？
因为浏览器和 Claude Code 配置了远端代理。**HTTP/SOCKS 代理协议默认在远端代理服务器上解析目标域名**，本机的 `/etc/hosts` 拦截的是本机的 DNS 与直连流量，代理通道内的数据直接打包走 Tailscale 隧道，因此不受阻断。

#### Q2: 我不想用了怎么还原？
直接运行：
```bash
sudo ./scripts/revert-hosts.sh
```
即刻完整恢复修改前的 `/etc/hosts`。

#### Q3: 为什么必须用独立 User Data Dir？
日常浏览器登录了大量的 Google 账号、国内插件与本地缓存，这些特征会被 Google Tag Manager / Cloudflare Turnstile 等脚本读取生成 Canvas 和扩展指纹。隔离沙箱能确保环境像一台刚刚拆封的崭新电脑。

---

## 📄 开源许可证

本项目基于 [MIT 许可证](LICENSE) 开源。仅供网络安全架构研究、企业内网合规代理配置与隐私保护学习参考。
