# Research: plugin models for channels (OpenClaw and others)

Read 2026-10-26. OpenClaw docs: https://docs.openclaw.ai (plugins guide /tools/plugin, architecture /plugins/architecture, manifest /plugins/manifest.md, channel SDK /plugins/sdk-channel-plugins, secrets /gateway/secrets).

## OpenClaw

The self-hosted personal AI assistant (formerly Clawdbot, then Moltbot). A long-running Node gateway is the control plane: chat app → channel adapter → gateway → agent runtime → tools → reply.

**Plugins**
- Native plugins: `openclaw.plugin.json` manifest + runtime module, running inside the gateway process. They register what they provide: `api.registerChannel`, `registerProvider`, tools, hooks, services, HTTP routes. "Compatible bundles" (Codex, Claude, Cursor layouts) are content packs: skills, commands and hooks.
- Manifest: required `id` and `configSchema` (JSON Schema); optional `channels`, `channelConfigs`, `providers`, `skills`, `uiHints` (marks sensitive fields), `requiresPlugins`. Parsed and validated without running plugin code. Code entry points and compat live in `package.json#openclaw` (`extensions`, `setupEntry`, `compat{pluginApi,minGatewayVersion}`).
- Discovery: bundled, workspace (off by default), global roots, `plugins.load.paths`. Install from ClawHub (default), `npm:`, `git:…@ref`, or a local path; `plugins update`; non-ClawHub sources need `--force` non-interactively.
- Channel API (`createChatChannelPlugin`): adapters for config, setup, security (DM policy, allow-from), pairing, threading, outbound (`sendText` → messageId, `sendMedia`, chunker), groups, actions, `agentPrompt`. Core owns one shared message tool. Capabilities are declared (`chatTypes`, `reactions`; `agentPrompt.questionInputMode: "buttons"` for native ask-the-user buttons); actions come from a closed vocabulary owned by core.
- Inbound: the plugin registers a webhook (`registerHttpRoute`, `auth: "plugin"`, verifying signatures itself) and dispatches into core; core owns the session-key shape; replies go back out tagged with the session key.
- Config at `plugins.entries.<id>.config`, with `plugins.allow` and `plugins.deny`. Secrets as SecretRefs (env, file, exec, store).
- Hot install, enable, disable and update without restart; load stages manifest → enablement → register → consumption, with rollback; channels drain before hot replacement.
- **No sandbox**: native plugins have core-level trust ("a malicious native plugin is equivalent to arbitrary code execution"). Plugin APIs are "experimental".

**Security incidents**
- ClawHavoc: hundreds of malicious ClawHub skills (341 of 2,857 per Koi; at least 1,184 over time per Antiy), mostly installing the AMOS macOS stealer via fake prerequisites; the only publishing gate was a week-old GitHub account. Mitigations: auto-hide after reports; VirusTotal scanning by hash plus daily re-scans ("not a silver bullet").
- CVE-2026-25253: the Control UI trusted a `gatewayUrl` query parameter and sent the gateway token to it; one-click RCE (CVSS 8.8), fixed in 2026.1.29.
- No code signing or provenance found; trust is allow/deny lists only.

## Other models

- **Apprise:** a `@notify(on="scheme")` decorator maps a URL scheme to a send function; one URL holds target and credentials. Very easy to add a channel.
- **Home Assistant:** `manifest.json` with pinned requirements, `config_flow`, `iot_class`, and a bronze-to-platinum quality scale.
- **n8n:** credentials in a separate typed file with `authenticate` injection and a test request, stored encrypted; "verified" community nodes installable only by owners or admins.

## Takeaways for yo-human channels

1. A manifest (`yo-human.channel.json`): id, apiVersion, config JSON Schema with secret fields marked, compat; readable without running code.
2. A closed, core-owned capability set (text, markdown, buttons, reply, media, urgency incl. break-through and critical, delivery confirmation, ack, quiet hours). Plugins can't invent new ones.
3. A small interface: `send(msg) → deliveryId`, optional inbound with plugin-side authenticity checks, `health()`, `testSend()`.
4. Core owns correlation IDs; plugins map provider IDs to them, so a reply from any channel resolves the same question.
5. Credentials apart from code, as secret references; a plugin never sees other plugins' secrets.
6. Plugins run out of process with a declared outbound-host allowlist (the step OpenClaw skipped).
7. Pinned versions, compat checks and checksums from day one; no open registry at launch; a verified tier later.
8. An Apprise-style URL shorthand for quick setup.
9. Hot enable/disable that drains in-flight deliveries; failed loads roll back and stay visible in diagnostics.
10. A quality tier per channel, so the router knows what it can escalate to.
