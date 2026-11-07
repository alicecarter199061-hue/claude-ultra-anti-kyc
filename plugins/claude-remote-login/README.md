# Claude Remote Login

A standalone CLIProxyAPI management plugin for enrolling a Claude account from another device. It uses the native C plugin ABI, adds its own resource page, and leaves the official core and UI unchanged.

## Why it exists

The official built-in Claude flow redirects to localhost. This plugin uses the hosted code-display callback so a teammate can sign in with Microsoft SSO on their own computer and send back the displayed `code#state`. It saves the result through `host.auth.save` as a native Claude credential; it does not implement or intercept inference.

## Installation

Install **Claude Remote Login** from the Plugin Store in the CLIProxyAPI management UI. To install manually, download the zip for your platform from the [latest release](https://github.com/Yousha-dev/claude-remote-login/releases/latest), extract `claude-remote-login.dll` / `.so` / `.dylib` into the proxy's plugin directory, and enable it:

```yaml
plugins:
  enabled: true
  configs:
    claude-remote-login:
      enabled: true
```

Open the plugin resource from Plugin Management or `/v0/resource/plugins/claude-remote-login/login`. Enter the proxy management key, generate a link, and submit the returned code. The HTML shell is public on the same listener; every state-changing endpoint requires the host's normal management authentication. Keys are not persisted in browser storage.

## API

All actions use authenticated JSON POST under `/v0/management/plugins/claude-remote-login`:

| Path | Body | Result |
| --- | --- | --- |
| `/start` | `{"mode":"setup_token"}` (optional, default: `"setup_token"`; or `"oauth"`) | `state`, `mode`, authorization `url`, `expires_at` |
| `/complete` | `{"state":"...","code":"code#state"}` | Connected account email |
| `/cancel` | `{"state":"..."}` | Cancels an idle pending session |

The native plugin resource and management prefixes intentionally retain their host-defined `/v0` paths even with the v8 core; the host's ordinary configuration API remains `/v8/management`.

Sessions last 15 minutes and are stored only in memory. PKCE verifiers and refresh/access tokens are never returned to the browser. Only the native host credential store persists completed credentials. A failed save can be retried in the same session without replaying the authorization code. Reauthorizing an existing native filename preserves its routing settings and device identity.

## Build and compatibility

The plugin is a separate Go module using only the Go standard library and the documented C ABI/JSON host callbacks. It does not depend on a patched CLIProxyAPI. Release builds use `go build -buildmode=c-shared` (Zig for Windows and Linux, clang for macOS); see `.github/workflows/release.yml`.

The C ABI shim follows the official dynamic-library example. Its upstream MIT notice is included in `UPSTREAM-LICENSE`.

- C ABI version: 1
- JSON registration schema: 6
- Host callbacks: `host.http.do`, `host.auth.list`, `host.auth.get`, `host.auth.save`
- Official host tested: CLIProxyAPI 8.0.47 on Windows AMD64

Core updates do not overwrite the plugin DLL or its source. A future core release that changes the public plugin contract may require updating the plugin; arbitrary future compatibility is not assumed.

## Tests

`CGO_ENABLED=0 go test ./...` runs the logic tests without a C compiler. Test cases use a fake host and never contact a provider or save fake credentials to the running proxy.
