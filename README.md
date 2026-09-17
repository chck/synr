# synr

`synr` previews, and with explicit approval leaves, inactive conversations in
Slack, Chatwork, and Zulip.

## Requirements

Install the published command with Go:

```bash
go install github.com/chck/synr/cmd/synr@latest
```

Source builds require Go 1.27. The repository checks are available through
`makers check`.

## Configuration

Credentials are read only from environment variables. Set the variables for the
one service you intend to scan:

```bash
# Slack
export SYNR_SLACK_TOKEN=...

# Chatwork
export SYNR_CHATWORK_TOKEN=...

# Zulip
export SYNR_ZULIP_URL=https://zulip.example
export SYNR_ZULIP_EMAIL=user@example.com
export SYNR_ZULIP_API_KEY=...
```

Optional, non-secret protected conversation IDs are read from
`${XDG_CONFIG_HOME:-$HOME/.config}/synr/config.yaml`. Its schema is exactly:

```yaml
services:
  slack:
    protected_channels:
      - C0123456789
  chatwork:
    protected_channels:
      - "123456789"
  zulip:
    protected_channels:
      - "42"
```

Unknown keys and malformed values are configuration errors. Conversation IDs
remain strings even when a service uses numeric IDs. The configuration file is
optional; `synr` never writes secrets to disk.

## Safety rules

`synr` is preview-only unless `--apply` is provided. A preview lists every
conversation with its service, name, stable ID, last activity, decision, and
reason. Only conversations with known activity strictly older than the cutoff
can be eligible.

- Slack: activity comes from the newest accessible message, and the workspace
  general channel is never left. The token needs the applicable
  `channels:history` and `groups:history` scopes; unavailable history stops the
  scan before any leave.
- Chatwork: sticky rooms, direct chats, and My Chat are never left. Only group
  rooms can be eligible; unknown room types stop the scan before any leave.
- Zulip: channels pinned by the current user are never unsubscribed.
- Every service also preserves IDs listed in `protected_channels`.

## Usage

Preview Slack conversations inactive for more than three months:

```bash
synr scan --service slack --before-months 3
```

After reviewing that preview, explicitly apply its eligible results:

```bash
synr scan --service slack --before-months 3 --apply
```

`--service` accepts `slack`, `chatwork`, or `zulip`. `--before-months` defaults
to `1` and must be a positive integer.

## Manual checklist

1. Set credentials only for the intended service and add every conversation
   that must be retained to `protected_channels`.
2. Run a preview without `--apply` and review every eligible row, including its
   stable ID and inactivity reason.
3. Deliberately select the live apply scope only after that review; if needed,
   protect all other conversations before continuing.
4. Run the matching command once with `--apply` and verify the resulting
   service state.
