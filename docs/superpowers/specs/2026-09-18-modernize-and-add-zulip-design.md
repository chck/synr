# Modernize synr and Add Zulip Support

## Purpose

Modernize the unsupported toolchain and dependencies, make destructive behavior
fail closed, and add Zulip as a supported service. Preserve Slack and Chatwork
support while replacing obsolete APIs and nondeterministic tests.

Discord support is outside this change. It is tracked in GitHub Issue #11
because its OAuth2 and explicit-selection workflow differs from inactivity-based
channel cleanup.

## User-facing behavior

The CLI has one operation:

```text
synr scan --service <slack|chatwork|zulip> [--before-months N] [--apply]
```

- `--service` is required.
- `--before-months` defaults to `1` and accepts integers greater than zero.
- Without `--apply`, the command only previews decisions.
- `--apply` is the explicit authorization to leave every eligible conversation
  shown by the preview. There is no additional interactive prompt, so the same
  command works in a terminal and in non-interactive automation.
- Output identifies each conversation by service, display name, stable ID, last
  activity time, decision, and reason.
- Help and validation run before credentials are loaded.
- Unknown services, invalid arguments, missing credentials, incomplete API data,
  and API failures result in a nonzero exit status and an actionable message.

## Configuration

Secrets are read only from environment variables:

| Service | Variables |
| --- | --- |
| Slack | `SYNR_SLACK_TOKEN` |
| Chatwork | `SYNR_CHATWORK_TOKEN` |
| Zulip | `SYNR_ZULIP_URL`, `SYNR_ZULIP_EMAIL`, `SYNR_ZULIP_API_KEY` |

Non-secret configuration is read from
`${XDG_CONFIG_HOME:-$HOME/.config}/synr/config.yaml`. The file is optional. Its
schema is:

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

Conversation identifiers are strings in the domain even when an upstream API
uses numeric IDs. Unknown keys and malformed values are configuration errors.
The legacy repository-relative `config/secrets.yaml` format is removed without
a compatibility fallback. Secrets are never written to disk by synr.

## Architecture

The implementation follows the repository's layered architecture guidance:

```text
cmd/synr/
internal/
├── presentation/cli/
├── usecase/
├── domain/
└── adapter/
    ├── config/
    ├── slack/
    ├── chatwork/
    └── zulip/
```

The domain owns immutable values for `Service`, `Conversation`, `Activity`, and
`Decision`. Constructors validate identifiers, names, and timestamps. A
conversation with missing or invalid activity cannot be represented as eligible
for departure.

Each service adapter implements a small provider interface:

```go
type Provider interface {
	List(context.Context) ([]domain.Conversation, error)
	Leave(context.Context, domain.Conversation) error
}
```

`List` returns all information needed for a decision. It never silently drops a
page, malformed item, unsupported state, or API error. `Leave` performs one
idempotent-as-supported upstream operation using the provider-specific stable
identifier or name and reports the conversation ID in its error context.

The use case evaluates every returned conversation against one cutoff time and
the protected-ID set. Presentation code parses arguments, selects an adapter,
renders decisions, and maps typed errors to exit statuses. Adapters contain no
CLI output or process exits.

## Eligibility rules

A conversation is eligible only when all of these conditions are true:

1. Its last activity timestamp is known and valid.
2. Its last activity is strictly before `now.AddDate(0, -beforeMonths, 0)`.
3. Its stable ID is not in the configured protected set.
4. No service-specific protection rule applies.

Service-specific protection rules are:

- Slack: never leave the workspace's general channel. Slack stars are not used,
  because Slack no longer adds newly saved items to `stars.list`.
- Chatwork: never leave sticky rooms or direct chats.
- Zulip: never unsubscribe from channels pinned by the current user.

Empty channels or rooms without a trustworthy timestamp are ineligible and are
shown with an `unknown activity` reason. Eligibility logic receives a clock so
tests do not depend on wall-clock time.

## Service adapters

### Slack

Use `github.com/slack-go/slack` v0.29.0 and the Conversations API. Paginate until
the cursor is exhausted, request channel metadata needed for the decision, and
use `conversations.leave` for mutation. The adapter must distinguish Slack API
errors from transport and decoding errors.

### Chatwork

Use a focused standard-library HTTP client for Chatwork API v2. Send the API
token only in the documented request header. Reuse one injected `http.Client`,
resolve endpoints from an injectable base URL, close every response body, and
accept only documented success statuses. Leaving a room uses the documented
form-encoded `action_type=leave` request.

### Zulip

Use a focused standard-library HTTP client with HTTP Basic authentication. List
the current user's subscriptions, then request the newest accessible message
for each channel using a channel narrow with `anchor=newest`. The message
timestamp is the channel activity time. Unsubscribe through
`DELETE /api/v1/users/me/subscriptions`.

All adapters use request contexts and a finite shared HTTP timeout. Rate limits,
server failures, authentication failures, malformed responses, and incomplete
pagination are returned as errors; this change does not add automatic retries.

## Apply sequencing and failure behavior

The command completes listing and decision-making before performing any
mutation. If any listing or metadata request fails, it prints no success claim
and performs no leaves.

With `--apply`, eligible conversations are processed in the deterministic order
shown in the preview. The first leave failure stops further mutations. The
summary distinguishes:

- successfully left conversations;
- the conversation whose leave failed;
- conversations not attempted after the failure.

The command exits nonzero after any partial failure. Preview mode never calls a
mutation endpoint.

## Dependencies and toolchain

- Go 1.27.x.
- `github.com/slack-go/slack` v0.29.0.
- `go.yaml.in/yaml/v3` v3.0.5.
- Go standard `flag`, `testing`, and `net/http/httptest` packages.

Remove `github.com/nlopes/slack`, `github.com/jessevdk/go-flags`, Ginkgo,
Gomega, and `gopkg.in/yaml.v2`. Transitive dependencies are generated by
`go mod tidy`; checksums are not edited manually.

## Testing

- Domain table tests cover every eligibility rule, cutoff boundaries, invalid
  values, and service-specific protections with a fixed clock.
- Use-case tests cover preview, apply, no-mutation-on-list-failure, deterministic
  ordering, and stop-on-first-leave-failure behavior with fake providers.
- Adapter tests use `httptest.Server` fixtures for authentication, pagination,
  encoding, status validation, malformed payloads, and successful leave calls.
- CLI tests cover help, argument validation, missing environment variables,
  config decoding, output, and exit statuses without real credentials.
- No default test contacts Slack, Chatwork, or Zulip.
- Manual end-to-end instructions document one preview and one intentionally
  selected apply verification per service. These remain human-owned because
  they require real accounts and destructive external actions.

## Automation and releases

Add `Makefile.toml` tasks invoked as `makers fmt`, `makers check`, `makers test`,
and `makers build`. `check` includes formatting verification, `go vet`, and
`govulncheck` v1.1.4. Add pre-commit hooks that run the repository's formatting
and check tasks, and install them in the development worktree.

Replace Travis CI with GitHub Actions using `actions/checkout@v7` and
`actions/setup-go@v7`. Pull requests and branch pushes run formatting, vet,
tests, build, and vulnerability scanning. Tag pushes use
`goreleaser/goreleaser-action@v7` with GoReleaser v2.18.2 and a
repository-owned GoReleaser configuration to create GitHub release artifacts.
CI and release workflows use Go 1.27.x.

Update the README with installation via `go install`, environment variables,
the non-secret YAML schema, preview/apply examples, supported services, and the
manual end-to-end checklist.

## Migration and rollback

This is an intentional breaking CLI and configuration migration. The forward
path is to move tokens from `config/secrets.yaml` into `SYNR_*` environment
variables and move protected IDs into the XDG config file. The previous command
shape and secret file do not remain as fallbacks, so an old invocation cannot
accidentally execute with new behavior.

Rollback is source-level: reinstall the previous release and restore its former
invocation/configuration separately. No repository migration mutates user data,
and preview is the default after upgrade. Actual service departures cannot be
rolled back by synr; this is why unknown state fails closed and `--apply` is
required.

## Out of scope

- Discord support (GitHub Issue #11).
- Concurrent leave operations.
- Automatic retry or backoff policy.
- Secure credential-store integration.
- A compatibility shim for the legacy CLI or secret file.
- Automated live-account end-to-end tests.
