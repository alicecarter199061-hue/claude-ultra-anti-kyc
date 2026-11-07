# ADR 0001: Claude 1年长效 Token 接入 CPA 网关决策

- **状态**: 已接受 (Accepted)
- **日期**: 2026-11-06
- **决策者**: 用户, Cindy, Gemini 4 Argon
- **受影响组件**: Sakura VPS CPA (`49.212.144.166`), `auth/` 凭据库, 本地指纹浏览器, 代理链路

---

## 1. 背景与问题陈述 (Context)

在采用 CPA (CLIProxyAPI) 网关代理 Claude 官方订阅推理时，常规的短期 OAuth 访问令牌（Access Token，约 8~24 小时过期）依赖单次刷新令牌（Refresh Token Rotation）。在多节点调试或网络抖动过程中，刷新竞争极易导致凭据失效（掉号），需反复频繁重新登录。

此外，用户希望在保证网关稳定性的同时，尽可能避免由于环境 IP 跳变而触发 Anthropic 针对 Claude 订阅的严格风控（KYC 封号）。

针对“如何在 CPA 网关上持续稳定维持 Claude 订阅会话”，存在以下可选技术路径：
1. **路径 A**：逆向开发或重新编译一个 CPA 的 Go 原生 `.so` 插件，在网关内部实现模拟 CLI 自动刷新与无头取号。
2. **路径 B**：使用官方 Claude CLI 提供的 `claude setup-token` 机制生成为期 1 年的持久令牌，通过合规的同源出口网络完成网页授权，并将长效令牌封装注入 CPA 的 `auth/` 目录。
3. **路径 C**：利用 CPA 内置的 `-claude-login` 命令或已有 `claude-remote-login.so` 插件直接在同出口下完成授权。

---

## 2. 架构决策 (Decision)

我们决定**采纳“C-ABI 动态库插件原生升级 + 服务端 CLI 冗余保障”**的双轮驱动方案：

1. **改造 `claude-remote-login` 插件为 v1.1.0 (C-ABI v1 共享库)**：
   - 经深入源码审阅，该插件并非 Go 内部的 `-buildmode=plugin`，而是标准的 **C-ABI (`-buildmode=c-shared`)** 动态库，具备独立 Go Runtime，零版本符号耦合；
   - 默认将授权流程切换为 `scope=user:inference` 与 `expires_in=31536000`，放行无 refresh_token 响应，跳过 profile 请求并生成确定性兜底 UUID；
   - 落盘时写入 `is_setup_token: true`、`skip_account_profile: true` 并将 `refresh_token` 置空，CPA 自动继承指纹并不触发后台刷新轮换；
   - UI 界面提供全中文引导与 1 年长效 / 标准 OAuth 模式切换。

2. **保留服务端 `cpa-setup-token` 独立命令作为双重保障**：
   - 作为无头 CLI 场景的备用方案，支持在 VPS 终端直接完成同源交互与落盘。

---

## 3. 架构影响与得失评估 (Consequences)

### 正面效益 (Positive)
- **免维护长周期运行**：1 年内无需重新登录，不会因网络丢包或并发请求引发 Refresh Token 竞争失效。
- **解耦热加载**：凭据独立保存在 `auth/` 目录，由 CPA 文件监听器秒级热识别，修改和新增凭据无需重启 CPA 服务或中断线上请求。
- **防封合规**：授权环境与调用出口完全锁定在日本樱花原生 IP 上，避免触发 Anthropic 地理跳变预警。
- **天然继承指纹伪装**：CPA 会自动为该令牌附加配置好的 `MacOS/arm64`、固定时区（Asia/Tokyo）与 CCH 请求签名，上游识别为正规官方 CLI 请求。

### 负面影响与取舍 (Trade-offs)
- **权限范围局限**：`claude setup-token` 签发的 Token 权限严格限定为 `user:inference`，仅适用于模型对话推理与代码生成，无法通过管理接口获取用户个人账户档案。网关代理场景完全满足。
- **不可完全自动化静默获取**：由于 Anthropic 在 OAuth 端点设置了 Cloudflare 防护与图形验证，任何合规令牌的生成都必须包含一次人工点击授权步骤，无法全自动化无人值守生成。

---

## 4. 执行与验证标准 (Compliance & Verification)

- 生成并导入的 `auth/claude-*.json` 必须包含合法的 ISO-8601 过期时间戳（当前时间 + 365 天）。
- 本地浏览器访问 Claude 授权端点时，必须校验出口 IP 严格为 VPS 原生 IP。
- 启动 CPA 后，通过 `http://<VPS_IP>:28317/management.html` 验证账号状态显示为健康可用。
