# Running one instance on a single Mac

This runs `yo-human-hosted` on one machine for local crews: agents on the same Mac reach the bus
over loopback. It is not the deployed setup (`setup.md`, a later slice of Task 12). Nothing leaves
the machine, and nothing outside it can reach the bus.

## What works today, and what does not

- Node tokens, the MCP server for agents (post, answers, replies, status) and the health check work.
- **Duet cannot connect until Task 10.** Duet reaches the bus through OAuth with GitHub, and that is not
  built yet, so every Duet request is refused.
- **There are no Bark nudges until Task 11.** The nudge worker is not built, so nothing is sent to
  your phone. You read the bus through Duet (later) or the CLI's `check` for counts.
- **There is no approval page until Tasks 10c and 10d, and no policy file until Task 10f.** Until the
  policy exists every approval is refused as not approvable.

## Inputs

- `DATA_DIR="<a directory only your account can read>"`: holds `bus.db`. The server creates it with
  mode 0700 and refuses one that is looser.
- `MCP_PORT="<a free port, such as 8081>"`: the MCP listener's port. It binds `127.0.0.1` only.
- `APPROVE_PORT="<a free port, such as 8080>"`: the approval listener's port, bound to loopback here.
  It must differ from `MCP_PORT`.
- `NODE_NAME="<a node name>"` and `CREW="<a crew name>"`: lowercase letters, digits and hyphens.

## Already set

Nothing: this is a new instance. Run `make build` in a checkout first; it writes
`dist/yo-human-hosted`.

## Steps

1. **Check the build.** The selfcheck opens a fresh database in a temporary directory, posts an
   approval as a node, records it as Duet and reads the answers. It makes no network call and reads
   no configuration:

   ```sh
   dist/yo-human-hosted selfcheck
   ```

2. **Set the environment.** The MCP address stays on loopback: `serve` refuses anything else and exits 78.
   `YH_PUBLIC_URL` is the address agents use, so it names the loopback listener and its port. The
   next three groups are placeholders, and that is exactly what they are. `LoadConfig` requires the
   GitHub, WebAuthn and approval-origin variables, but nothing reads their values yet: OAuth is
   Task 10 and the approval page is Task 10c. Do not put a real GitHub secret here. When those
   tasks land, replace these values:

   ```sh
   export YH_DB_PATH="$DATA_DIR/bus.db"
   export YH_LISTEN_MCP="127.0.0.1:$MCP_PORT"
   export YH_LISTEN_APPROVE="127.0.0.1:$APPROVE_PORT"
   export YH_PUBLIC_URL="http://127.0.0.1:$MCP_PORT"

   # Placeholders until Tasks 10 and 10c: required by LoadConfig, not used yet.
   export YH_OWNER_GITHUB_ID="1"
   export YH_GITHUB_CLIENT_ID="local-placeholder"
   export YH_GITHUB_CLIENT_SECRET="GITHUB-SECRET-FAKE"
   export YH_WEBAUTHN_RP_ID="approve.example.com"
   export YH_APPROVE_ORIGIN="https://approve.example.com"
   ```

   Every other variable keeps its default (`YH_TOOL_DEADLINE` 5 s, `YH_NUDGE_*` and `YH_BARK_*`
   unset, and so on).

3. **Mint a token for each node.** The token is printed once, and only the token. Pipe it into your
   password manager and never paste it into a file or a chat:

   ```sh
   dist/yo-human-hosted token create --name "$NODE_NAME" --swarm "$CREW" | <password manager's store command>
   ```

   `token list` shows names and a short prefix, and `token revoke --name "$NODE_NAME"` ends one.

4. **Run the server.** It stays in the foreground. SIGINT or SIGTERM drains requests in flight for up
   to 10 s, then cuts what is left, and exits 0 either way:

   ```sh
   dist/yo-human-hosted serve
   ```

5. **Check it.** From another terminal, with the same environment:

   ```sh
   curl -s "http://127.0.0.1:$APPROVE_PORT/healthz"    # ok
   curl -s "http://127.0.0.1:$MCP_PORT/healthz"        # ok
   dist/yo-human-hosted check                          # integrity: ok, and the row counts
   ```

An agent connects to `http://127.0.0.1:$MCP_PORT/mcp` with `Authorization: Bearer <its node token>`.
The request's `Host` must be the host of `YH_PUBLIC_URL` (`127.0.0.1:$MCP_PORT`), which any client
that uses that URL sends. The MCP listener answers 404 to every other host.
