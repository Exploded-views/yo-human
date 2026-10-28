# Research: notification services, free tiers and iOS Critical Alerts

Checked 2026-10-25/26 by web research. Vendor pages rarely carry dates; treat as current-as-fetched. A search miss is not proof.

## Verdict

Nothing on the market cleanly does what yo-human does. Every candidate is send-only, approval-only, paid, or lacks iOS Critical Alerts, and none combines severity rules, dedup and resolve, rate limits, quiet hours and an acknowledgement or reply the agent can read back.

## Comparison

| Service | Free? | iOS Critical Alerts on free | Ack/reply read-back | API | Notes |
|---|---|---|---|---|---|
| PagerDuty Free | Yes: 5 users, 1 schedule, 1 escalation policy | Very likely: the iOS app has "Critical Alerts for High-Urgency" (bypass Focus and silent). No doc restricts it to paid; confirm in the app | Yes: incident state over REST; Events v2 trigger/ack/resolve with dedup_key | Full REST + Events v2 | Includes 100 SMS/phone notifications a month. Its MCP integration is paid-only |
| ntfy (ntfy.sh or self-hosted) | Yes (paid tiers exist) | No: feature request #1235 open; server PR #1908 unreviewed | Partial: view/http action buttons can hit a webhook | Simple HTTP | Self-hosted needs upstream-base-url for instant iOS |
| Pushover | $4.99 once per platform | Yes, opt-in, for priority 2 | Best built-in: priority-2 retries, a receipt to poll, a callback URL, cancel_by_tag | 10k msgs/month | Not free |
| Bark | Free, MIT, self-hostable | Yes (level=critical) | None (one-way) | GET/POST | iOS only |
| Home Assistant Companion | Free if you run HA | Yes | Yes: actionable buttons and text-input replies | HA REST/WebSocket | Requires running HA |
| Grafana Cloud IRM Free | 3 users | Toggle documented; unconfirmed on Free | Via IRM API | Yes | OnCall OSS archived 2026-03-24 |
| Opsgenie | End of sale 2025-06-04, shutdown 2027-04-05 | n/a | n/a | n/a | Dead |
| incident.io Basic | Free | Not stated | n/a | No API/webhooks on Basic | |
| Better Stack Free | Slack/email only | No | n/a | No on-call | |
| Squadcast, Zenduty | No free plan | n/a | | | |

## Existing "agent → human" tools (none fit)

- HumanLayer: pivoted to an agent-running platform; old approval SDK and channels gone.
- gotoHuman: approval forms via web inbox, email and Slack; no push or critical alerts.
- PagerDuty MCP server: the local repo is archived; the hosted one is read-only by default and paid.
- ntfy MCP servers: send with buttons; one wait-for-reply limited by MCP timeouts; no critical alerts.
- Pushover MCPs: send-only, or via a Composio account.
- paging-mcp (Apprise-based, MIT): severities but no ack, dedup, rate limits or quiet hours.
- Claude Code hooks (claude-remote-approver, claude-push): permission prompts only, via ntfy.

## Apple constraints

- **Critical Alerts** need Apple's entitlement, granted per app on request (mostly to medical, safety and home-security apps). It must be in the signing profile, so there's no TestFlight or sideload route around it, and EU alternative marketplaces don't change it (and don't apply in the US). PagerDuty's app has it.
- **Time-sensitive** interruption level needs no approval; it breaks through Focus, not the silent switch.
