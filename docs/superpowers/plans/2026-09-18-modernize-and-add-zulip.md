# Modernize synr and Add Zulip Support Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the unsafe Go 1.12 implementation with a fail-closed Go 1.27 CLI that previews Slack, Chatwork, and Zulip cleanup decisions and mutates only with `--apply`.

**Architecture:** Immutable domain values make invalid or unknown activity explicit, a use case evaluates and applies decisions through a small provider interface, and service/config adapters isolate external APIs. The presentation layer parses `synr scan`, renders deterministic output, and maps typed failures to nonzero exit statuses.

**Tech Stack:** Go 1.27.1, standard `flag`/`testing`/`httptest`, `github.com/slack-go/slack` v0.29.0, `go.yaml.in/yaml/v3` v3.0.5, cargo-make, GitHub Actions, GoReleaser v2.18.2.

**Spec:** `docs/superpowers/specs/2026-09-18-modernize-and-add-zulip-design.md`

## Global Constraints

- Go version is `1.27.1`; `go.mod` declares `go 1.27` and CI selects `1.27.x`.
- Preview is the default. Only `--apply` authorizes external mutation.
- `--service` is required; `--before-months` defaults to `1` and must be greater than zero.
- Secrets come only from the documented `SYNR_*` environment variables and are never written by synr.
- Missing, malformed, incomplete, or failed upstream data is never eligible for departure.
- Finish all listing and decision work before the first leave request; stop on the first leave failure.
- Slack general channels, Chatwork sticky/direct rooms, Zulip pinned channels, and configured protected IDs are ineligible.
- Do not add Discord code; Discord remains tracked by GitHub Issue #11.
- Do not add automatic retries, concurrent leaves, legacy CLI/config fallbacks, or live-account CI tests.
- All repository commands run through `makers`; use `mise exec go@1.27.1 -- makers <task>` where Go is not already active.
- Every commit includes `Co-Authored-By: Codex <noreply@openai.com>`.

---

## File Structure

- `cmd/synr/main.go`: process bootstrap only.
- `internal/domain/service.go`: supported-service enum and parser.
- `internal/domain/conversation.go`: validated IDs, activity, protection, and immutable conversation values.
- `internal/domain/decision.go`: cutoff/protection eligibility rules.
- `internal/usecase/scan.go`: list, evaluate, sort, preview, and sequential apply orchestration.
- `internal/adapter/config/config.go`: strict XDG YAML and per-service environment credentials.
- `internal/adapter/chatwork/client.go`: Chatwork v2 HTTP provider.
- `internal/adapter/zulip/client.go`: Zulip subscription/message HTTP provider.
- `internal/adapter/slack/client.go`: maintained Slack SDK provider.
- `internal/presentation/cli/run.go`: `scan` parsing, provider selection, output, and exit mapping.
- `Makefile.toml`, `.mise.toml`, `.pre-commit-config.yaml`: reproducible local commands and hooks.
- `.github/workflows/ci.yml`, `.github/workflows/release.yml`, `.goreleaser.yaml`: CI and releases.
- `README.md`: current installation, configuration, and manual verification.

---

### Task 1: Reproducible Toolchain and Domain Model

**Files:**
- Create: `.mise.toml`
- Create: `Makefile.toml`
- Create: `internal/domain/service.go`
- Create: `internal/domain/conversation.go`
- Create: `internal/domain/decision.go`
- Create: `internal/domain/decision_test.go`
- Modify: `go.mod:1-14`
- Delete: `slack_test.go`
- Delete: `synr_suite_test.go`

**Interfaces:**
- Produces: `domain.ParseService(string) (domain.Service, error)`.
- Produces: `domain.NewConversation(Service, string, string, Activity, Protection) (Conversation, error)`.
- Produces: `domain.KnownActivity(time.Time) (Activity, error)` and `domain.UnknownActivity() Activity`.
- Produces: `domain.Evaluate(Conversation, time.Time, map[ConversationID]struct{}) Decision`.

- [ ] **Step 1: Add the pinned toolchain and command launcher**

Create `.mise.toml`:

```toml
[tools]
go = "1.27.1"
```

Create `Makefile.toml` with `fmt`, `fmt-check`, `vet`, `test`, `build`, `vuln`, and aggregate `check` tasks. Use these exact commands:

```toml
[tasks.fmt]
command = "go"
args = ["fmt", "./..."]

[tasks.fmt-check]
script = '''
files="$(gofmt -l $(git ls-files '*.go'))"
test -z "$files" || { echo "$files"; exit 1; }
'''

[tasks.vet]
command = "go"
args = ["vet", "./..."]

[tasks.test]
command = "go"
args = ["test", "-race", "./..."]

[tasks.build]
command = "go"
args = ["build", "./..."]

[tasks.vuln]
command = "go"
args = ["run", "golang.org/x/vuln/cmd/govulncheck@v1.1.4", "./..."]

[tasks.check]
dependencies = ["fmt-check", "vet", "test", "build", "vuln"]
```

Change `go.mod` to `go 1.27` without changing dependency requirements yet.

- [ ] **Step 2: Replace the live Slack suite with failing domain tests**

Delete the credential-dependent Ginkgo files and add table tests covering exact outcomes:

```go
func TestEvaluate(t *testing.T) {
	now := time.Date(2026, time.September, 18, 12, 0, 0, 0, time.UTC)
	cutoff := now.AddDate(0, -1, 0)
	old, _ := KnownActivity(cutoff.Add(-time.Second))
	recent, _ := KnownActivity(cutoff)
	tests := []struct {
		name       string
		activity   Activity
		protection Protection
		configured bool
		eligible   bool
		reason     DecisionReason
	}{
		{name: "old", activity: old, eligible: true, reason: ReasonInactive},
		{name: "cutoff is not old", activity: recent, reason: ReasonActive},
		{name: "unknown", activity: UnknownActivity(), reason: ReasonUnknownActivity},
		{name: "service protected", activity: old, protection: ProtectionGeneral, reason: ReasonServiceProtected},
		{name: "configured", activity: old, configured: true, reason: ReasonConfiguredProtected},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			conversation, err := NewConversation(ServiceSlack, "C123", "general", tt.activity, tt.protection)
			if err != nil {
				t.Fatal(err)
			}
			protected := map[ConversationID]struct{}{}
			if tt.configured {
				protected[conversation.ID()] = struct{}{}
			}
			decision := Evaluate(conversation, cutoff, protected)
			if decision.Eligible() != tt.eligible || decision.Reason() != tt.reason {
				t.Fatalf("got eligible=%v reason=%q", decision.Eligible(), decision.Reason())
			}
		})
	}
}
```

Also assert that empty IDs, empty names, zero known timestamps, and unsupported service strings return errors.

- [ ] **Step 3: Run the domain test to verify it fails**

Run: `mise exec go@1.27.1 -- makers test`

Expected: FAIL because the `internal/domain` types do not exist; the prior clean-checkout secret-file failure is gone.

- [ ] **Step 4: Implement validated immutable domain values**

Use unexported fields with getters. Define these exact enums:

```go
type Service string
const (
	ServiceSlack Service = "slack"
	ServiceChatwork Service = "chatwork"
	ServiceZulip Service = "zulip"
)

type Protection string
const (
	ProtectionNone Protection = ""
	ProtectionGeneral Protection = "general"
	ProtectionSticky Protection = "sticky"
	ProtectionDirect Protection = "direct"
	ProtectionPinned Protection = "pinned"
)

type DecisionReason string
const (
	ReasonInactive DecisionReason = "inactive"
	ReasonActive DecisionReason = "active"
	ReasonUnknownActivity DecisionReason = "unknown activity"
	ReasonServiceProtected DecisionReason = "service protected"
	ReasonConfiguredProtected DecisionReason = "configured protected"
)
```

`Evaluate` must check configured protection, service protection, known activity, then strict cutoff ordering. Return a `Decision` containing the original conversation, eligibility, and reason.

- [ ] **Step 5: Format and verify the domain**

Run: `mise exec go@1.27.1 -- makers fmt`

Run: `mise exec go@1.27.1 -- makers test`

Expected: PASS with no external API calls.

- [ ] **Step 6: Commit**

```bash
git add .mise.toml Makefile.toml go.mod internal/domain slack_test.go synr_suite_test.go
git commit -m "refactor: add validated cleanup domain

Co-Authored-By: Codex <noreply@openai.com>"
```

---

### Task 2: Fail-closed Scan and Apply Use Case

**Files:**
- Create: `internal/usecase/scan.go`
- Create: `internal/usecase/scan_test.go`

**Interfaces:**
- Consumes: `domain.Conversation`, `domain.Decision`, and `domain.Evaluate` from Task 1.
- Produces: `usecase.Provider` with `List(context.Context) ([]domain.Conversation, error)` and `Leave(context.Context, domain.Conversation) error`.
- Produces: `usecase.Scan(context.Context, Provider, Request, Clock) (Result, error)`.
- `Request` fields: `BeforeMonths int`, `Apply bool`, `ProtectedIDs map[domain.ConversationID]struct{}`.
- `Result` fields: `Decisions []domain.Decision`, `Left []domain.Conversation`, `Failed *domain.Conversation`, `NotAttempted []domain.Conversation`.

- [ ] **Step 1: Write failing orchestration tests with fakes**

Implement a recording fake and fixed clock, then cover:

```go
func TestScanDoesNotLeaveInPreview(t *testing.T)
func TestScanDoesNotLeaveWhenListFails(t *testing.T)
func TestScanSortsDecisionsByNameThenID(t *testing.T)
func TestScanAppliesOnlyEligibleConversations(t *testing.T)
func TestScanStopsAfterFirstLeaveFailure(t *testing.T)
func TestScanRejectsNonPositiveMonths(t *testing.T)
```

For stop-on-failure, provide eligible conversations `alpha`, `beta`, `gamma`, fail `beta`, and assert calls are exactly `alpha`, `beta`; `Result.Left` is `alpha`, `Result.Failed` is `beta`, and `Result.NotAttempted` is `gamma`.

- [ ] **Step 2: Run tests to verify failure**

Run: `mise exec go@1.27.1 -- makers test`

Expected: FAIL because `usecase.Scan` and its types are undefined.

- [ ] **Step 3: Implement two-phase orchestration**

Define:

```go
type Clock interface { Now() time.Time }
type Provider interface {
	List(context.Context) ([]domain.Conversation, error)
	Leave(context.Context, domain.Conversation) error
}
```

`Scan` validates the month count, calls `List` once, evaluates all items using one clock value, sorts decisions by `Name()` then `ID()`, returns immediately in preview mode, and sequentially leaves only eligible items in apply mode. Wrap list failures as `list conversations: %w` and leave failures as `leave <service> conversation <id>: %w` while returning the populated partial result.

- [ ] **Step 4: Verify use-case behavior**

Run: `mise exec go@1.27.1 -- makers fmt`

Run: `mise exec go@1.27.1 -- makers test`

Expected: PASS, including exact call-order assertions.

- [ ] **Step 5: Commit**

```bash
git add internal/usecase
git commit -m "refactor: add fail-closed cleanup workflow

Co-Authored-By: Codex <noreply@openai.com>"
```

---

### Task 3: Strict XDG Configuration and Environment Credentials

**Files:**
- Create: `internal/adapter/config/config.go`
- Create: `internal/adapter/config/config_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Produces: `config.Load(path string) (Config, error)`; an empty `path` resolves the XDG/default path.
- Produces: `Config.ProtectedIDs(domain.Service) map[domain.ConversationID]struct{}`.
- Produces: `config.CredentialsFor(domain.Service, func(string) string) (Credentials, error)`.
- `Credentials` fields: `Token`, `URL`, `Email`, `APIKey`; only the fields required by the selected service are populated.

- [ ] **Step 1: Add failing configuration tests**

Cover absent optional files, strict decoding, ID validation, XDG resolution, and per-service credentials:

```go
func TestLoadMissingFileReturnsEmptyConfig(t *testing.T)
func TestLoadRejectsUnknownYAMLKey(t *testing.T)
func TestLoadReturnsProtectedIDsByService(t *testing.T)
func TestDefaultPathUsesXDGConfigHome(t *testing.T)
func TestCredentialsForLoadsOnlySelectedService(t *testing.T)
func TestCredentialsForReportsMissingVariable(t *testing.T)
```

Use `t.TempDir()` and `t.Setenv`; never create a real token file. Assert that a Slack lookup does not require any Chatwork or Zulip variable.

- [ ] **Step 2: Run the package test to verify failure**

Run: `mise exec go@1.27.1 -- go test ./internal/adapter/config -v`

Expected: FAIL because the config adapter is undefined.

- [ ] **Step 3: Implement strict config decoding**

Add `go.yaml.in/yaml/v3 v3.0.5`. Model explicit `slack`, `chatwork`, and `zulip` service keys, each with `protected_channels []string`. Decode with `yaml.Decoder.KnownFields(true)`, reject blank IDs, and ensure a second YAML document is not present. Treat `os.ErrNotExist` as an empty config; propagate all other read/decode errors with the path.

Resolve the default path as `$XDG_CONFIG_HOME/synr/config.yaml`, otherwise `os.UserConfigDir()/synr/config.yaml`. Read these exact variables only for the selected service:

```text
SYNR_SLACK_TOKEN
SYNR_CHATWORK_TOKEN
SYNR_ZULIP_URL
SYNR_ZULIP_EMAIL
SYNR_ZULIP_API_KEY
```

Trim surrounding whitespace for validation but do not log values.

- [ ] **Step 4: Verify configuration behavior**

Run: `mise exec go@1.27.1 -- makers fmt`

Run: `mise exec go@1.27.1 -- go test ./internal/adapter/config -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/adapter/config
git commit -m "feat: load strict non-secret service configuration

Co-Authored-By: Codex <noreply@openai.com>"
```

---

### Task 4: Safe Chatwork Adapter

**Files:**
- Create: `internal/adapter/chatwork/client.go`
- Create: `internal/adapter/chatwork/client_test.go`

**Interfaces:**
- Consumes: `domain.NewConversation` and `usecase.Provider`.
- Produces: `chatwork.New(token string, baseURL *url.URL, httpClient *http.Client) (*Client, error)`.
- `Client` implements `usecase.Provider`.

- [ ] **Step 1: Write failing HTTP contract tests**

Use `httptest.Server` to cover:

```go
func TestListBuildsConversationsAndProtectsStickyAndDirectRooms(t *testing.T)
func TestListRejectsNonSuccessStatus(t *testing.T)
func TestListRejectsMalformedJSON(t *testing.T)
func TestListTreatsZeroTimestampAsUnknown(t *testing.T)
func TestLeaveSendsFormEncodedAction(t *testing.T)
func TestLeaveRejectsUnexpectedStatus(t *testing.T)
```

In the success fixture, return one old group, one sticky group, one direct room,
and one zero-timestamp group. Assert `X-ChatWorkToken`, request paths, protection values, and `action_type=leave`. Expect HTTP 204 for leave.

- [ ] **Step 2: Run the adapter test to verify failure**

Run: `mise exec go@1.27.1 -- go test ./internal/adapter/chatwork -v`

Expected: FAIL because `chatwork.Client` is undefined.

- [ ] **Step 3: Implement the Chatwork v2 client**

Reuse the injected client and base URL. `List` calls `GET rooms`; `Leave` calls `DELETE rooms/{escaped-id}` with `application/x-www-form-urlencoded`. Check request-construction errors before accessing the request, close every response body immediately after a successful round trip, cap diagnostic error-body reads, and accept only HTTP 200 for list and HTTP 204 for leave.

Map `room_id` to a decimal string, use `last_update_time` seconds for known activity, and map `sticky`/`type == "direct"` to the matching domain protection.

- [ ] **Step 4: Verify Chatwork behavior**

Run: `mise exec go@1.27.1 -- makers fmt`

Run: `mise exec go@1.27.1 -- go test ./internal/adapter/chatwork -v`

Expected: PASS and no network access beyond `httptest.Server`.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/chatwork
git commit -m "refactor: replace Chatwork client with safe HTTP adapter

Co-Authored-By: Codex <noreply@openai.com>"
```

---

### Task 5: Zulip Subscription Adapter

**Files:**
- Create: `internal/adapter/zulip/client.go`
- Create: `internal/adapter/zulip/client_test.go`

**Interfaces:**
- Consumes: `domain.NewConversation`, activity/protection values, and `usecase.Provider`.
- Produces: `zulip.New(baseURL *url.URL, email, apiKey string, httpClient *http.Client) (*Client, error)`.
- `Client` implements `usecase.Provider`; `Leave` uses `Conversation.Name()` because Zulip's unsubscribe endpoint requires channel names.

- [ ] **Step 1: Write failing Zulip HTTP tests**

Cover:

```go
func TestListFetchesNewestMessageForEverySubscription(t *testing.T)
func TestListProtectsPinnedSubscription(t *testing.T)
func TestListTreatsChannelWithoutMessagesAsUnknown(t *testing.T)
func TestListFailsIfAnyActivityRequestFails(t *testing.T)
func TestLeaveSendsEncodedChannelName(t *testing.T)
func TestRequestsUseBasicAuthentication(t *testing.T)
```

The fixture must validate `GET /api/v1/users/me/subscriptions`, then one `GET /api/v1/messages` per channel with `anchor=newest`, `num_before=1`, `num_after=0`, and example JSON narrow `[{"operator":"channel","operand":"Denmark"}]`. The leave fixture validates `DELETE /api/v1/users/me/subscriptions` and form value `subscriptions=["Denmark"]`.

- [ ] **Step 2: Run the Zulip test to verify failure**

Run: `mise exec go@1.27.1 -- go test ./internal/adapter/zulip -v`

Expected: FAIL because `zulip.Client` is undefined.

- [ ] **Step 3: Implement sequential, fail-closed Zulip reads**

Validate an absolute HTTP(S) base URL and nonblank email/API key. Decode subscription fields `stream_id`, `name`, and `pin_to_top`. Query newest messages sequentially in the returned subscription order; any transport, status, or decode failure aborts `List` without returning partial data. A missing message yields unknown activity; a positive UNIX timestamp yields known activity. Accept Zulip responses only when HTTP status is 2xx and JSON `result` is `success`.

For unsubscribe, send Basic auth and the exact JSON-array form value derived from `Conversation.Name()`; validate the `removed`/`not_removed` success response so an ignored name is observable.

- [ ] **Step 4: Verify Zulip behavior**

Run: `mise exec go@1.27.1 -- makers fmt`

Run: `mise exec go@1.27.1 -- go test ./internal/adapter/zulip -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/adapter/zulip
git commit -m "feat: add Zulip channel cleanup adapter

Co-Authored-By: Codex <noreply@openai.com>"
```

---

### Task 6: Maintained Slack Conversations Adapter

**Files:**
- Create: `internal/adapter/slack/client.go`
- Create: `internal/adapter/slack/client_test.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: `domain.NewConversation`, activity/protection values, and `usecase.Provider`.
- Produces: `slack.New(token string, httpClient *http.Client, apiURL string) (*Client, error)`.
- `Client` wraps `github.com/slack-go/slack.Client` and implements `usecase.Provider`.

- [ ] **Step 1: Add failing paginated Slack contract tests**

Use the SDK's `OptionAPIURL` with `httptest.Server` and cover:

```go
func TestListPaginatesMemberConversationsAndFetchesInfo(t *testing.T)
func TestListProtectsGeneralChannel(t *testing.T)
func TestListTreatsMissingLastReadAsUnknown(t *testing.T)
func TestListFailsOnPaginationError(t *testing.T)
func TestLeaveUsesConversationsLeave(t *testing.T)
func TestLeaveReportsSlackAPIError(t *testing.T)
```

Return two `conversations.list` pages with cursors, include a non-member channel to assert it is skipped, and validate one `conversations.info` call per member. Use Slack timestamps such as `1789718400.000000`; invalid or empty timestamps must become unknown activity without epoch fallback.

- [ ] **Step 2: Run the Slack package test to verify failure**

Run: `mise exec go@1.27.1 -- go test ./internal/adapter/slack -v`

Expected: FAIL because the new adapter is undefined.

- [ ] **Step 3: Implement the maintained Slack adapter**

Add `github.com/slack-go/slack v0.29.0`. Build the SDK with `OptionHTTPClient` and `OptionAPIURL`; do not enable its retry options. Iterate `GetConversationsContext` with `Limit: 200`, `ExcludeArchived: true`, and types `public_channel`, `private_channel`; stop only on an empty next cursor. Fetch each member conversation with `GetConversationInfoContext`.

Parse the integer seconds before the decimal point with `strconv.ParseInt`. An empty or invalid `last_read` yields unknown activity. Map `IsGeneral` to `ProtectionGeneral`. `Leave` calls `LeaveConversationContext` and treats an SDK error as failure; `not_in_channel` is successful idempotent completion.

- [ ] **Step 4: Verify Slack behavior**

Run: `mise exec go@1.27.1 -- makers fmt`

Run: `mise exec go@1.27.1 -- go test ./internal/adapter/slack -v`

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add go.mod go.sum internal/adapter/slack
git commit -m "refactor: migrate Slack cleanup to Conversations API

Co-Authored-By: Codex <noreply@openai.com>"
```

---

### Task 7: CLI Presentation, Bootstrap, and Legacy Removal

**Files:**
- Create: `internal/presentation/cli/run.go`
- Create: `internal/presentation/cli/run_test.go`
- Create: `cmd/synr/main.go`
- Delete: `main.go`
- Delete: `chatwork/chatwork.go`
- Delete: `chatwork/rooms.go`
- Delete: `chatwork/util.go`
- Delete: `config/config.go`
- Delete: `config/secrets.yaml.copy`
- Delete: `slack/slack.go`
- Modify: `go.mod`
- Modify: `go.sum`

**Interfaces:**
- Consumes: config credentials/protected IDs, all three provider constructors, and `usecase.Scan`.
- Produces: `cli.Run(ctx context.Context, args []string, stdout, stderr io.Writer, dependencies Dependencies) int`.
- `Dependencies` contains `Getenv func(string) string`, `ConfigPath string`, `HTTPClient *http.Client`, `Clock usecase.Clock`, and `NewProvider ProviderFactory` so tests never contact live APIs.
- Produces: `cli.DefaultDependencies() Dependencies` with `os.Getenv`, the default config path, a 30-second HTTP client, the real clock, and production adapter constructors.
- `cmd/synr/main.go` calls `os.Exit(cli.Run(context.Background(), os.Args[1:], os.Stdout, os.Stderr, cli.DefaultDependencies()))`.

- [ ] **Step 1: Write failing CLI tests**

Use injected writers, environment lookup, a temporary XDG directory, and an injectable provider factory. Cover:

```go
func TestRunShowsHelpWithoutCredentials(t *testing.T)
func TestRunRequiresScanSubcommand(t *testing.T)
func TestRunRequiresSupportedService(t *testing.T)
func TestRunRejectsNonPositiveBeforeMonths(t *testing.T)
func TestRunPreviewNeverLeaves(t *testing.T)
func TestRunApplyPrintsPartialFailureSummary(t *testing.T)
func TestRunLoadsOnlySelectedServiceCredentials(t *testing.T)
```

Assert deterministic tabular lines containing service, name, ID, RFC3339 activity or `unknown`, decision, and reason. Assert exit `0` for help/success, `2` for usage/configuration errors, and `1` for API/apply failures.

- [ ] **Step 2: Run CLI tests to verify failure**

Run: `mise exec go@1.27.1 -- go test ./internal/presentation/cli -v`

Expected: FAIL because `cli.Run` is undefined.

- [ ] **Step 3: Implement `scan` parsing and provider construction**

Use a dedicated `flag.FlagSet` with output directed to the injected stderr. Parse only:

```text
scan --service <slack|chatwork|zulip> --before-months <positive integer> --apply
```

Parse and validate before loading configuration or credentials. Use a 30-second `http.Client` and pass it to the selected adapter. Load protected IDs only after service validation. Render all decisions before rendering apply results; never print credential values.

- [ ] **Step 4: Replace the bootstrap and remove legacy packages**

Add `cmd/synr/main.go`, delete the old entry point/client/config files, then run:

Run: `mise exec go@1.27.1 -- go mod tidy`

Expected: `go.mod` retains only direct requirements `github.com/slack-go/slack v0.29.0` and `go.yaml.in/yaml/v3 v3.0.5`; Ginkgo, Gomega, go-flags, nlopes/slack, and yaml.v2 are absent.

- [ ] **Step 5: Verify the integrated CLI**

Run: `mise exec go@1.27.1 -- makers fmt`

Run: `mise exec go@1.27.1 -- makers test`

Run: `mise exec go@1.27.1 -- makers build`

Run: `mise exec go@1.27.1 -- go run ./cmd/synr --help`

Expected: all checks PASS; help exits without credentials; no test contacts a live service.

- [ ] **Step 6: Commit**

```bash
git add cmd internal/presentation go.mod go.sum main.go chatwork config slack
git commit -m "feat: expose safe multi-service scan command

Co-Authored-By: Codex <noreply@openai.com>"
```

---

### Task 8: Hooks, CI, Releases, and Documentation

**Files:**
- Create: `.pre-commit-config.yaml`
- Create: `.github/workflows/ci.yml`
- Create: `.github/workflows/release.yml`
- Create: `.goreleaser.yaml`
- Modify: `README.md`
- Delete: `.travis.yml`

**Interfaces:**
- Consumes: `makers fmt-check`, `makers vet`, `makers test`, `makers build`, and `makers vuln`.
- Produces: pull-request/push CI and `v*` tag release artifacts for `./cmd/synr`.

- [ ] **Step 1: Add local hooks and install them**

Create local pre-commit hooks with `language: system`, `pass_filenames: false`, and these entries:

```yaml
repos:
  - repo: local
    hooks:
      - id: makers-fmt-check
        name: makers fmt-check
        entry: makers fmt-check
        language: system
        pass_filenames: false
      - id: makers-check
        name: makers check
        entry: makers check
        language: system
        pass_filenames: false
```

Run: `pre-commit install`

Expected: the worktree's `.git/hooks/pre-commit` is installed.

- [ ] **Step 2: Add CI and release workflows**

CI triggers on pull requests and pushes to `master`, uses `ubuntu-latest`, `actions/checkout@v7`, `actions/setup-go@v7` with `go-version: 1.27.x`, installs cargo-make 0.37.24 with `cargo install --locked cargo-make --version 0.37.24`, and runs `makers check`.

Release triggers on pushed tags `v*`, uses the same checkout/setup actions, and runs `goreleaser/goreleaser-action@v7` with `version: v2.18.2`, `args: release --clean`, and `GITHUB_TOKEN: ${{ secrets.GITHUB_TOKEN }}`.

Configure `.goreleaser.yaml` with schema `version: 2`, project name `synr`, main package `./cmd/synr`, `CGO_ENABLED=0`, targets `linux`/`darwin` for `amd64`/`arm64`, tar.gz archives, SHA-256 checksums, and changelog filters excluding `docs:`, `test:`, and `chore:` commits.

- [ ] **Step 3: Rewrite README against the implemented CLI**

Document:

```bash
go install github.com/chck/synr/cmd/synr@latest
synr scan --service slack --before-months 3
synr scan --service slack --before-months 3 --apply
```

Include all five `SYNR_*` variables, the exact XDG YAML schema from the spec, service-specific protection rules, preview/apply safety, Go 1.27 requirement for source builds, and a manual checklist that requires preview review before a deliberately selected live apply. Remove Travis, OAuth-test-token, `go get -u`, and repository-relative secret-file instructions.

- [ ] **Step 4: Run the complete local gate**

Run: `mise exec go@1.27.1 -- makers check`

Run: `pre-commit run --all-files`

Run: `git diff --check`

Expected: PASS with no formatting output, race failures, vet findings, build errors, or reachable known vulnerabilities.

- [ ] **Step 5: Validate release configuration**

Run: `docker run --rm -v "$PWD:/src" -w /src goreleaser/goreleaser:v2.18.2 check`

Run: `docker run --rm -v "$PWD:/src" -w /src goreleaser/goreleaser:v2.18.2 release --snapshot --clean`

Expected: configuration check PASS and snapshot archives/checksums appear under ignored `dist/` without publishing.

- [ ] **Step 6: Commit**

```bash
git add .pre-commit-config.yaml .github .goreleaser.yaml README.md .travis.yml
git commit -m "ci: modernize verification and release automation

Co-Authored-By: Codex <noreply@openai.com>"
```

---

### Task 9: Final Compatibility and Safety Verification

**Files:**
- Modify only files required by failures found in this task.

**Interfaces:**
- Verifies every interface and global constraint from Tasks 1-8.

- [ ] **Step 1: Audit dependencies and forbidden legacy paths**

Run: `mise exec go@1.27.1 -- go list -m all`

Run: `rg -n 'nlopes/slack|jessevdk/go-flags|onsi/ginkgo|onsi/gomega|gopkg.in/yaml.v2|config/secrets.yaml|GetChannelInfo|GetChannels' --glob '!docs/superpowers/**' .`

Expected: only the intended current modules are direct dependencies and the repository search returns no legacy code/documentation references.

- [ ] **Step 2: Prove preview cannot mutate**

Run: `mise exec go@1.27.1 -- go test ./internal/presentation/cli ./internal/usecase -run 'Preview|DoesNotLeave|ListFails' -count=20`

Expected: PASS on all repetitions with fake leave-call counts at zero.

- [ ] **Step 3: Run the full verification suite fresh**

Run: `mise exec go@1.27.1 -- go clean -testcache`

Run: `mise exec go@1.27.1 -- makers check`

Run: `git diff --check`

Run: `git status --short`

Expected: all checks PASS; status contains only intentional final-task fixes, or is clean if none were needed.

- [ ] **Step 4: Commit only if verification required fixes**

```bash
git add cmd internal go.mod go.sum Makefile.toml .mise.toml .pre-commit-config.yaml .github .goreleaser.yaml README.md
git commit -m "fix: resolve final modernization verification failures

Co-Authored-By: Codex <noreply@openai.com>"
```

If no files changed, do not create an empty commit.
