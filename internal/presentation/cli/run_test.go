package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/chck/synr/internal/adapter/config"
	"github.com/chck/synr/internal/domain"
	"github.com/chck/synr/internal/usecase"
)

type fixedClock struct{}

func (fixedClock) Now() time.Time {
	return time.Date(2026, 9, 18, 0, 0, 0, 0, time.UTC)
}

type recordingProvider struct {
	conversations []domain.Conversation
	left          []string
	listErr       error
	failID        string
}

func (provider *recordingProvider) List(context.Context) ([]domain.Conversation, error) {
	return provider.conversations, provider.listErr
}

func (provider *recordingProvider) Leave(_ context.Context, conversation domain.Conversation) error {
	provider.left = append(provider.left, conversation.ID().Value())
	if conversation.ID().Value() == provider.failID {
		return errors.New("permission denied")
	}
	return nil
}

func testDependencies(t *testing.T, provider usecase.Provider) Dependencies {
	t.Helper()
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	return Dependencies{
		Getenv: func(key string) string {
			if key == "SYNR_SLACK_TOKEN" {
				return "test-token"
			}
			return ""
		},
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Clock:      fixedClock{},
		NewProvider: func(domain.Service, config.Credentials, *http.Client) (usecase.Provider, error) {
			return provider, nil
		},
	}
}

func conversation(t *testing.T, name string, id string, timestamp string) domain.Conversation {
	t.Helper()
	activity := domain.UnknownActivity()
	if timestamp != "" {
		at, err := time.Parse(time.RFC3339, timestamp)
		if err != nil {
			t.Fatal(err)
		}
		activity, err = domain.KnownActivity(at)
		if err != nil {
			t.Fatal(err)
		}
	}
	value, err := domain.NewConversation(domain.ServiceSlack, id, name, activity, domain.ProtectionNone)
	if err != nil {
		t.Fatal(err)
	}
	return value
}

func TestRunShowsHelpWithoutCredentials(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"scan", "--help"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), args, &stdout, &stderr, Dependencies{}); code != 0 {
				t.Fatalf("exit = %d, stderr = %s", code, &stderr)
			}
			for _, want := range []string{"scan", "--service", "--before-months", "--apply"} {
				if !strings.Contains(stdout.String(), want) {
					t.Errorf("help missing %q: %s", want, &stdout)
				}
			}
			if stderr.Len() != 0 {
				t.Errorf("help stderr = %s", &stderr)
			}
		})
	}
}

func assertUsageError(t *testing.T, args []string) {
	t.Helper()
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), args, &stdout, &stderr, Dependencies{}); code != 2 {
		t.Fatalf("exit = %d, want 2; stderr = %s", code, &stderr)
	}
	if stdout.Len() != 0 || stderr.Len() == 0 {
		t.Fatalf("stdout = %q, stderr = %q", &stdout, &stderr)
	}
}

func TestRunRequiresScanSubcommand(t *testing.T) {
	for _, args := range [][]string{nil, {"slack"}, {"--service", "slack"}, {"scan", "extra", "--service", "slack"}, {"scan", "--service", "slack", "--unknown"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { assertUsageError(t, args) })
	}
}

func TestRunRequiresSupportedService(t *testing.T) {
	for _, args := range [][]string{{"scan"}, {"scan", "--service", "discord"}, {"scan", "--service"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) { assertUsageError(t, args) })
	}
}

func TestRunRejectsNonPositiveBeforeMonths(t *testing.T) {
	for _, value := range []string{"0", "-1", "invalid"} {
		t.Run(value, func(t *testing.T) {
			assertUsageError(t, []string{"scan", "--service", "slack", "--before-months", value})
		})
	}
}

func TestRunPreviewNeverLeaves(t *testing.T) {
	provider := &recordingProvider{conversations: []domain.Conversation{
		conversation(t, "unknown", "C3", ""),
		conversation(t, "same", "C2", "2026-08-18T00:00:00Z"),
		conversation(t, "same", "C1", "2026-08-17T23:59:59Z"),
	}}
	dependencies := testDependencies(t, provider)
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"scan", "--service", "slack"}, &stdout, &stderr, dependencies); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, &stderr)
	}
	want := "service\tname\tid\tactivity\tdecision\treason\n" +
		"slack\tsame\tC1\t2026-08-17T23:59:59Z\teligible\tinactive\n" +
		"slack\tsame\tC2\t2026-08-18T00:00:00Z\tskip\tactive\n" +
		"slack\tunknown\tC3\tunknown\tskip\tunknown activity\n"
	if stdout.String() != want {
		t.Errorf("output = %q, want %q", &stdout, want)
	}
	if len(provider.left) != 0 {
		t.Errorf("preview left %v", provider.left)
	}
}

func TestRunApplyPrintsPartialFailureSummary(t *testing.T) {
	provider := &recordingProvider{failID: "C2", conversations: []domain.Conversation{
		conversation(t, "third", "C3", "2026-01-01T00:00:00Z"),
		conversation(t, "first", "C1", "2026-01-01T00:00:00Z"),
		conversation(t, "second", "C2", "2026-01-01T00:00:00Z"),
	}}
	dependencies := testDependencies(t, provider)
	var stdout, stderr bytes.Buffer
	code := Run(context.Background(), []string{"scan", "--service", "slack", "--apply"}, &stdout, &stderr, dependencies)
	if code != 1 {
		t.Fatalf("exit = %d, stderr = %s", code, &stderr)
	}
	wantSuffix := "result\tservice\tname\tid\nleft\tslack\tfirst\tC1\nfailed\tslack\tsecond\tC2\nnot attempted\tslack\tthird\tC3\n"
	if !strings.HasSuffix(stdout.String(), wantSuffix) {
		t.Errorf("missing partial summary: %s", &stdout)
	}
	if strings.Count(stdout.String(), "\teligible\tinactive\n") != 3 {
		t.Errorf("missing decisions: %s", &stdout)
	}
	if !strings.Contains(stderr.String(), "permission denied") {
		t.Errorf("missing failure: %s", &stderr)
	}
	if !reflect.DeepEqual(provider.left, []string{"C1", "C2"}) {
		t.Errorf("leave attempts = %v", provider.left)
	}
}

func TestRunLoadsOnlySelectedServiceCredentials(t *testing.T) {
	for _, service := range []domain.Service{domain.ServiceSlack, domain.ServiceChatwork, domain.ServiceZulip} {
		t.Run(string(service), func(t *testing.T) {
			provider := &recordingProvider{}
			dependencies := testDependencies(t, provider)
			values := map[string]string{}
			wantCredentials := config.Credentials{Token: "selected-token"}
			switch service {
			case domain.ServiceSlack:
				values["SYNR_SLACK_TOKEN"] = "selected-token"
			case domain.ServiceChatwork:
				values["SYNR_CHATWORK_TOKEN"] = "selected-token"
			case domain.ServiceZulip:
				values = map[string]string{"SYNR_ZULIP_URL": "https://zulip.example", "SYNR_ZULIP_EMAIL": "user@example.com", "SYNR_ZULIP_API_KEY": "selected-key"}
				wantCredentials = config.Credentials{URL: "https://zulip.example", Email: "user@example.com", APIKey: "selected-key"}
			}
			dependencies.Getenv = func(key string) string {
				value, ok := values[key]
				if !ok {
					t.Fatalf("read unselected credential %s", key)
				}
				return value
			}
			dependencies.NewProvider = func(got domain.Service, credentials config.Credentials, client *http.Client) (usecase.Provider, error) {
				if got != service || credentials != wantCredentials || client != dependencies.HTTPClient {
					t.Fatal("incorrect provider dependencies")
				}
				return provider, nil
			}
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), []string{"scan", "--service", string(service)}, &stdout, &stderr, dependencies); code != 0 {
				t.Fatalf("exit = %d, stderr = %s", code, &stderr)
			}
		})
	}
}

func TestRunLoadsProtectedIDsFromDefaultConfig(t *testing.T) {
	provider := &recordingProvider{conversations: []domain.Conversation{conversation(t, "protected", "C1", "2026-01-01T00:00:00Z")}}
	dependencies := testDependencies(t, provider)
	path := filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "synr", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("services:\n  slack:\n    protected_channels: [C1]\n"), 0600); err != nil {
		t.Fatal(err)
	}
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"scan", "--service", "slack", "--apply"}, &stdout, &stderr, dependencies); code != 0 {
		t.Fatalf("exit = %d, stderr = %s", code, &stderr)
	}
	if !strings.Contains(stdout.String(), "\tskip\tconfigured protected\n") || len(provider.left) != 0 {
		t.Fatalf("protected channel mishandled: %s; left %v", &stdout, provider.left)
	}
}

func TestRunConfigurationErrors(t *testing.T) {
	for _, scenario := range []string{"credentials", "config", "provider"} {
		t.Run(scenario, func(t *testing.T) {
			dependencies := testDependencies(t, &recordingProvider{})
			switch scenario {
			case "credentials":
				dependencies.Getenv = func(string) string { return "" }
			case "config":
				dependencies.ConfigPath = filepath.Join(t.TempDir(), "invalid.yaml")
				if err := os.WriteFile(dependencies.ConfigPath, []byte("unknown: value\n"), 0600); err != nil {
					t.Fatal(err)
				}
			case "provider":
				dependencies.NewProvider = func(domain.Service, config.Credentials, *http.Client) (usecase.Provider, error) {
					return nil, errors.New("invalid provider settings")
				}
			}
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), []string{"scan", "--service", "slack"}, &stdout, &stderr, dependencies); code != 2 {
				t.Fatalf("exit = %d, stderr = %s", code, &stderr)
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Fatalf("stdout = %s, stderr = %s", &stdout, &stderr)
			}
		})
	}
}

func TestRunListingFailureDoesNotClaimSuccessOrExposeCredentials(t *testing.T) {
	provider := &recordingProvider{listErr: errors.New("request failed with test-token")}
	dependencies := testDependencies(t, provider)
	var stdout, stderr bytes.Buffer
	if code := Run(context.Background(), []string{"scan", "--service", "slack", "--apply"}, &stdout, &stderr, dependencies); code != 1 {
		t.Fatalf("exit = %d", code)
	}
	if stdout.Len() != 0 || strings.Contains(stderr.String(), "test-token") || !strings.Contains(stderr.String(), "request failed") {
		t.Fatalf("stdout = %q, stderr = %q", &stdout, &stderr)
	}
	if len(provider.left) != 0 {
		t.Fatalf("left after listing failure: %v", provider.left)
	}
}

type fixtureTransport func(*http.Request) (*http.Response, error)

func (transport fixtureTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return transport(request)
}

func TestDefaultDependenciesBuildSelectedAdapters(t *testing.T) {
	for _, service := range []string{"slack", "chatwork", "zulip"} {
		t.Run(service, func(t *testing.T) {
			t.Setenv("XDG_CONFIG_HOME", t.TempDir())
			t.Setenv("SYNR_SLACK_TOKEN", "slack-token")
			t.Setenv("SYNR_CHATWORK_TOKEN", "chatwork-token")
			t.Setenv("SYNR_ZULIP_URL", "https://zulip.example")
			t.Setenv("SYNR_ZULIP_EMAIL", "user@example.com")
			t.Setenv("SYNR_ZULIP_API_KEY", "zulip-key")
			dependencies := DefaultDependencies()
			if dependencies.HTTPClient.Timeout != 30*time.Second {
				t.Fatalf("timeout = %v", dependencies.HTTPClient.Timeout)
			}
			if delta := time.Since(dependencies.Clock.Now()); delta < -time.Second || delta > time.Second {
				t.Fatalf("clock delta = %v", delta)
			}
			dependencies.HTTPClient.Transport = fixtureTransport(func(request *http.Request) (*http.Response, error) {
				body := ""
				switch service {
				case "slack":
					if err := request.ParseForm(); err != nil {
						return nil, err
					}
					if request.URL.String() != "https://slack.com/api/conversations.list" || request.Form.Get("token") != "slack-token" {
						return nil, fmt.Errorf("incorrect Slack request")
					}
					body = `{"ok":true,"channels":[],"response_metadata":{"next_cursor":""}}`
				case "chatwork":
					if request.URL.String() != "https://api.chatwork.com/v2/rooms" || request.Header.Get("X-ChatWorkToken") != "chatwork-token" {
						return nil, fmt.Errorf("incorrect Chatwork request")
					}
					body = `[]`
				case "zulip":
					email, key, ok := request.BasicAuth()
					if request.URL.String() != "https://zulip.example/api/v1/users/me/subscriptions" || !ok || email != "user@example.com" || key != "zulip-key" {
						return nil, fmt.Errorf("incorrect Zulip request")
					}
					body = `{"result":"success","subscriptions":[]}`
				}
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: request}, nil
			})
			var stdout, stderr bytes.Buffer
			if code := Run(context.Background(), []string{"scan", "--service", service}, &stdout, &stderr, dependencies); code != 0 {
				t.Fatalf("exit = %d, stderr = %s", code, &stderr)
			}
		})
	}
}
