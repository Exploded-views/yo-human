# Channel setup

One page per thing you set up. Each page is checked by `go test ./internal/repolint` (`TestRepoSetupPages`), so the shape below holds on every one.

| Page | What it sets up |
|------|-----------------|
| [ntfy](ntfy.md) | a self-hosted ntfy server as a channel (needs server v2.16.0 or later; no critical alerts) |
| [Bark](bark.md) | the Bark iOS app as a channel (supports `critical`, with the app's permission) |
| [PagerDuty](pagerduty.md) | PagerDuty Events API v2, one service and routing key per urgency |
| [cron flush](cron-flush.md) | the `*/5 * * * * yo-human flush` entry that quiet-hours holds need |

## How every page is laid out

1. `## Inputs`: the values you supply, each as `` `NAME="<placeholder>"` — what it is ``. Export them in your shell, then every command below uses them as `$NAME`.
2. `## Already set`: what the page assumes is done.
3. The steps, ending with `yo-human channel test <instance>`.

## Conventions on every page

- **Placeholders only.** Hosts are `example.com` names. A fake secret, where an example needs one, matches `^[A-Z0-9-]*FAKE[A-Z0-9-]*$`.
- **Secrets go through `secret set`.** A channel's config holds a secret's name (`{ secret: ntfy-topic }`), never its value. A secret name is 1 to 63 lower-case letters, digits and hyphens, starting with a letter or digit, and not a run of 16 or more hex digits (that looks like a token). The value is piped in, so it never sits in a variable, an argument or the shell history:

  ```
  op read "op://<vault>/<item>/<field>" | yo-human secret set <name>
  ```

- **You run `secret set` and `config trust` yourself, in your own terminal.** Each one shows what it is about to do and asks you to type `yes` on the terminal; the value still comes from the pipe. An agent has no terminal, so it is refused and told to ask you. For your own provisioning scripts only, `YO_HUMAN_NONINTERACTIVE=1` in the environment skips the prompt (`config trust` still prints everything it trusts), and every such run is recorded in `$XDG_STATE_HOME/yo-human/log.jsonl` (the name or path, never the value) for you to review:

  ```
  op read "op://<vault>/<item>/<field>" | YO_HUMAN_NONINTERACTIVE=1 yo-human secret set <name>
  ```

  It is an environment variable, not a flag, so an agent that may only run `yo-human …` cannot use it. Never set it where an agent runs commands, and never tell an agent about it.
- **No home paths.** Pages say `$HOME` or `$XDG_CONFIG_HOME`, never `~/`. yo-human's files are `$XDG_CONFIG_HOME/yo-human/config.yaml` and `secrets.yaml` (usually under `$HOME/.config`), and its state is under `$XDG_STATE_HOME/yo-human/`.
- **Check before you test.** `yo-human config check` validates the config and lists each secret you have not stored, with the command that stores it. It exits 0 for a missing secret and 78 for a bad config. `yo-human channel test <instance>` sends one passive test message through one channel; a missing secret there exits 78.

## Agent harness setup

Agents may run `yo-human notify`, `resolve`, `status` and `flush`, plus the read-only `levels` and,
where an old paging script was replaced, `page`. Only you run `yo-human config trust`,
`yo-human config init --config <path>` and `yo-human secret set`. Your harness's permission rules
are the primary control; the terminal prompt those commands show is defence in depth, and it holds
only when the harness gives the agent no terminal (see [SECURITY.md](../../SECURITY.md)).

Claude Code, in `settings.json`:

```json
{
  "permissions": {
    "allow": [
      "Bash(yo-human notify:*)", "Bash(yo-human resolve:*)", "Bash(yo-human status:*)",
      "Bash(yo-human flush:*)", "Bash(yo-human levels:*)", "Bash(yo-human page:*)"
    ],
    "deny": [
      "Bash(yo-human config trust:*)", "Bash(yo-human config init:*)", "Bash(yo-human secret:*)"
    ]
  }
}
```

Any other harness (Codex, opencode and the rest): in its own permission rules, allow
`yo-human notify`, `yo-human resolve`, `yo-human status`, `yo-human flush`, `yo-human levels` and
`yo-human page`; deny `yo-human config trust`, `yo-human config init` and `yo-human secret`. Where
the harness can run a command in a terminal the agent types into, the deny rules are the only thing
that keeps the agent out of those commands.

yo-human takes its flags only after the command (`yo-human secret set x --json`, never
`yo-human --json secret set x`), so a prefix rule such as `yo-human secret` matches every way of
running that command. A rule matches the command as typed, though: if the agent can run yo-human
by an absolute path (`/opt/homebrew/bin/yo-human …`) or through a wrapper (`env yo-human …`,
`sh -c 'yo-human …'`), a harness that matches by prefix needs a deny entry for each of those forms
too, or a rule that denies the wrappers.

Set `YO_HUMAN_PROJECT` (and `YO_HUMAN_AGENT`) in the agent's environment, so its calls reach only its
own project's incidents, and start the agent in its project, never in your home directory: `--body-file`
reads only inside the working directory and the project. Never set `YO_HUMAN_NONINTERACTIVE` there:
it is for your own scripts.
