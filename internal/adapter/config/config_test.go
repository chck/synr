package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/chck/synr/internal/domain"
)

func TestLoadMissingFileReturnsEmptyConfig(t *testing.T) {
	config, err := Load(filepath.Join(t.TempDir(), "missing.yaml"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	if got := config.ProtectedIDs(domain.ServiceSlack); len(got) != 0 {
		t.Errorf("ProtectedIDs(slack) = %v, want empty", got)
	}
}

func TestLoadRequiresServicesMapping(t *testing.T) {
	for _, test := range []struct {
		name    string
		content string
	}{
		{name: "empty document mapping", content: "{}\n"},
		{name: "null document", content: "null\n"},
		{name: "null services", content: "services: null\n"},
		{name: "sequence services", content: "services: []\n"},
		{name: "scalar services", content: "services: value\n"},
	} {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Load(writeConfig(t, test.content)); err == nil {
				t.Fatal("Load accepted a document without a services mapping")
			}
		})
	}
}

func TestLoadAcceptsEmptyServicesMapping(t *testing.T) {
	configuration, err := Load(writeConfig(t, "services: {}\n"))
	if err != nil {
		t.Fatal(err)
	}
	if got := configuration.ProtectedIDs(domain.ServiceSlack); len(got) != 0 {
		t.Errorf("ProtectedIDs(slack) = %v, want empty", got)
	}
}

func TestLoadRejectsUnknownYAMLKey(t *testing.T) {
	path := writeConfig(t, "unknown: true\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want unknown YAML key error")
	}
	if !strings.Contains(err.Error(), "unknown") {
		t.Errorf("Load() error = %q, want unknown key context", err)
	}
}

func TestLoadRejectsAdditionalYAMLDocument(t *testing.T) {
	path := writeConfig(t, "services:\n  slack: {}\n---\nservices:\n  chatwork: {}\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want multiple YAML documents error")
	}
	if !strings.Contains(err.Error(), "multiple") {
		t.Errorf("Load() error = %q, want multiple document context", err)
	}
}

func TestLoadReturnsProtectedIDsByService(t *testing.T) {
	path := writeConfig(t, "services:\n  slack:\n    protected_channels:\n      - C123\n  chatwork:\n    protected_channels:\n      - room-456\n  zulip:\n    protected_channels:\n      - stream-789\n")

	config, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}

	for _, test := range []struct {
		service domain.Service
		id      string
	}{
		{service: domain.ServiceSlack, id: "C123"},
		{service: domain.ServiceChatwork, id: "room-456"},
		{service: domain.ServiceZulip, id: "stream-789"},
	} {
		t.Run(string(test.service), func(t *testing.T) {
			if _, ok := config.ProtectedIDs(test.service)[conversationID(t, test.service, test.id)]; !ok {
				t.Errorf("ProtectedIDs(%q) does not include %q", test.service, test.id)
			}
		})
	}
}

func TestLoadRejectsBlankProtectedID(t *testing.T) {
	path := writeConfig(t, "services:\n  slack:\n    protected_channels:\n      - '   '\n")

	_, err := Load(path)
	if err == nil {
		t.Fatal("Load() error = nil, want blank protected ID error")
	}
	if !strings.Contains(err.Error(), "protected") {
		t.Errorf("Load() error = %q, want protected ID context", err)
	}
}

func TestDefaultPathUsesXDGConfigHome(t *testing.T) {
	xdgConfigHome := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdgConfigHome)
	path := filepath.Join(xdgConfigHome, "synr", "config.yaml")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("services:\n  slack:\n    protected_channels:\n      - C123\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	config, err := Load("")
	if err != nil {
		t.Fatalf("Load(\"\") error = %v", err)
	}
	if _, ok := config.ProtectedIDs(domain.ServiceSlack)[conversationID(t, domain.ServiceSlack, "C123")]; !ok {
		t.Error("Load(\"\") did not read the XDG configuration path")
	}
}

func TestCredentialsForLoadsOnlySelectedService(t *testing.T) {
	t.Setenv("SYNR_SLACK_TOKEN", "test-slack-token")
	lookups := make([]string, 0, 1)

	credentials, err := CredentialsFor(domain.ServiceSlack, func(name string) string {
		lookups = append(lookups, name)
		return os.Getenv(name)
	})
	if err != nil {
		t.Fatalf("CredentialsFor() error = %v", err)
	}

	want := Credentials{Token: "test-slack-token"}
	if !reflect.DeepEqual(credentials, want) {
		t.Errorf("CredentialsFor() = %#v, want %#v", credentials, want)
	}
	if !reflect.DeepEqual(lookups, []string{"SYNR_SLACK_TOKEN"}) {
		t.Errorf("credential lookups = %v, want only SYNR_SLACK_TOKEN", lookups)
	}
}

func TestLoadRejectsServicesOutsideWrapper(t *testing.T) {
	for _, service := range []string{"slack", "chatwork", "zulip"} {
		t.Run(service, func(t *testing.T) {
			_, err := Load(writeConfig(t, service+": {}\n"))
			if err == nil {
				t.Fatal("Load accepted a service outside the services wrapper")
			}
		})
	}
}

func TestLoadRejectsUnknownNestedKeys(t *testing.T) {
	for _, content := range []string{"services:\n  discord: {}\n", "services:\n  slack:\n    unknown: true\n"} {
		_, err := Load(writeConfig(t, content))
		if err == nil {
			t.Fatalf("Load accepted unknown nested key: %s", content)
		}
	}
}

func TestCredentialsForReportsMissingVariable(t *testing.T) {
	_, err := CredentialsFor(domain.ServiceZulip, func(string) string { return "   " })
	if err == nil {
		t.Fatal("CredentialsFor() error = nil, want missing variable error")
	}
	if !strings.Contains(err.Error(), "SYNR_ZULIP_URL") {
		t.Errorf("CredentialsFor() error = %q, want missing variable name", err)
	}
}

func writeConfig(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.yaml")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func conversationID(t *testing.T, service domain.Service, value string) domain.ConversationID {
	t.Helper()
	conversation, err := domain.NewConversation(service, value, "test", domain.UnknownActivity(), domain.ProtectionNone)
	if err != nil {
		t.Fatal(err)
	}
	return conversation.ID()
}
