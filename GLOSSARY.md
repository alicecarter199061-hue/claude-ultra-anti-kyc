# 领域术语表 (Domain Glossary)

本文档定义 Claude Ultra 隔离盾与 CPA (CLIProxyAPI) 网关集成方案的统一术语体系。

---

### 1. 认证与凭据类 (Authentication & Credentials)

- **Claude 1-Year Setup-Token (1年长效订阅令牌)**  
  通过 Anthropic 官方 CLI 命令 `claude setup-token` 签发的高持久性 OAuth 访问令牌（通常以 `sk-ant-oat01-` 开头）。默认具备 `user:inference` 作用域，有效期为 365 天，期间不依赖刷新令牌（Refresh Token）轮换，适用于长期自动化代理与无头网关调用。

- **Refresh Token Rotation (刷新令牌单次轮换机制)**  
  Anthropic OAuth 2.0 规范中采用的安全策略。每次使用旧 `refresh_token` 换取新 `access_token` 时，服务端都会签发全新的 `refresh_token` 并立即使旧令牌作废。若多实例并发或不同机器同时尝试使用相同旧令牌刷新，将触发冲突并导致账号凭据完全失效。

- **Console API Key (控制台商业密钥)**  
  在 Anthropic Console (`console.anthropic.com`) 中按用量付费充值后创建的永久密钥（以 `sk-ant-api03-` 开头）。与订阅制 OAuth 令牌不同，该密钥按 Token 消耗计费，不享有 Claude Pro/Team 订阅的固定套餐额度。

- **CPA Auth JSON (网关账号凭据清单)**  
  CPA 网关位于 `auth/` 目录下的持久化 JSON 规范文件（如 `claude-<uuid>.json`），记录访问令牌、刷新令牌、过期时间戳、关联组织标识与优先级权重，供网关文件监听器热加载和调度。

---

### 2. 架构与网关组件类 (Architecture & Components)

- **CPA (CLIProxyAPI)**  
  开源的高性能 AI 提供商网关与协议转换器，能够将客户端发起的标准 OpenAI / Anthropic / Gemini 协议请求，伪装并代理转发至各大上游官方 CLI / Web 服务。

- **Claude Remote Login Plugin (Claude 远程登录插件)**  
  CPA 加载的 Go 原生动态链接库扩展 (`claude-remote-login.so`)。在无图形界面的服务器上提供 Web 端 OAuth 桥接，引导管理员完成浏览器鉴权并将回调捕获注入网关凭据库。

- **Tailscale Internal Mesh (Tailscale 虚拟内网)**  
  基于 WireGuard 协议构建的点对点加密覆盖网络。为客户端与海外 VPS 分配专用的 `100.64.0.0/10` 虚拟 IP，使代理流量完全绕过公网扫描与裸机暴露。

---

### 3. 指纹风控与网络安全类 (Fingerprint & Security)

- **Same-Egress Authorization (同源出口授权原则)**  
  安全防封黄金准则：用户在浏览器或 CLI 中点击授权并换取 OAuth Token 时的公网出口 IP，必须与网关后续发起日常推理请求的出口 IP 保持 100% 同源一致。严禁在中国大陆本地网络签发 Token 后投递至海外 VPS 代理使用。

- **Wire Fingerprint (线缆级指纹伪装)**  
  CPA 在向上游 `api.anthropic.com/v1/messages` 转发流量时注入的底层协议与请求头特征，包括 Client Challenge Hash (CCH) 动态签名、特定 Anthropic-Beta 头、稳定设备 UUID 以及操作系统（MacOS/arm64）元数据。

- **Happy Eyeballs Dual-Stack Bypass (双栈竞速绕过)**  
  现代操作系统与浏览器在同时探测到 IPv4 与 IPv6 解析时，优先尝试建立延迟更低的连接通道。若黑洞拦截仅封禁 IPv4 (`127.0.0.1`)，流量将直接通过 IPv6 (`AAAA` 记录) 泄露至公网，造成代理逃逸。
