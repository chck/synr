package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/chck/synr/internal/domain"
	"go.yaml.in/yaml/v3"
)

const applicationName = "synr"

type Credentials struct {
	Token  string
	URL    string
	Email  string
	APIKey string
}

type Config struct {
	protectedIDs map[domain.Service]map[domain.ConversationID]struct{}
}

type fileConfig struct {
	Services servicesConfig `yaml:"services"`
}

type servicesConfig struct {
	Slack    serviceConfig `yaml:"slack"`
	Chatwork serviceConfig `yaml:"chatwork"`
	Zulip    serviceConfig `yaml:"zulip"`
}

type serviceConfig struct {
	ProtectedChannels []string `yaml:"protected_channels"`
}

func Load(path string) (Config, error) {
	if path == "" {
		var err error
		path, err = defaultPath()
		if err != nil {
			return Config{}, err
		}
	}

	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return emptyConfig(), nil
	}
	if err != nil {
		return Config{}, fmt.Errorf("open configuration %q: %w", path, err)
	}
	defer file.Close()

	decoder := yaml.NewDecoder(file)
	decoder.KnownFields(true)
	var decoded fileConfig
	if err := decoder.Decode(&decoded); err != nil {
		return Config{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	if err := rejectAdditionalDocument(decoder); err != nil {
		return Config{}, fmt.Errorf("decode configuration %q: %w", path, err)
	}
	return newConfig(decoded)
}

func (config Config) ProtectedIDs(service domain.Service) map[domain.ConversationID]struct{} {
	protected := config.protectedIDs[service]
	copy := make(map[domain.ConversationID]struct{}, len(protected))
	for id := range protected {
		copy[id] = struct{}{}
	}
	return copy
}

func CredentialsFor(service domain.Service, lookup func(string) string) (Credentials, error) {
	if lookup == nil {
		return Credentials{}, fmt.Errorf("environment variable lookup must not be nil")
	}

	switch service {
	case domain.ServiceSlack:
		token, err := requiredCredential("SYNR_SLACK_TOKEN", lookup)
		if err != nil {
			return Credentials{}, err
		}
		return Credentials{Token: token}, nil
	case domain.ServiceChatwork:
		token, err := requiredCredential("SYNR_CHATWORK_TOKEN", lookup)
		if err != nil {
			return Credentials{}, err
		}
		return Credentials{Token: token}, nil
	case domain.ServiceZulip:
		url, err := requiredCredential("SYNR_ZULIP_URL", lookup)
		if err != nil {
			return Credentials{}, err
		}
		email, err := requiredCredential("SYNR_ZULIP_EMAIL", lookup)
		if err != nil {
			return Credentials{}, err
		}
		apiKey, err := requiredCredential("SYNR_ZULIP_API_KEY", lookup)
		if err != nil {
			return Credentials{}, err
		}
		return Credentials{URL: url, Email: email, APIKey: apiKey}, nil
	default:
		return Credentials{}, fmt.Errorf("unsupported service %q", service)
	}
}

func defaultPath() (string, error) {
	if configHome := os.Getenv("XDG_CONFIG_HOME"); configHome != "" {
		return filepath.Join(configHome, applicationName, "config.yaml"), nil
	}
	configHome, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user configuration directory: %w", err)
	}
	return filepath.Join(configHome, applicationName, "config.yaml"), nil
}

func rejectAdditionalDocument(decoder *yaml.Decoder) error {
	var additionalDocument any
	err := decoder.Decode(&additionalDocument)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err != nil {
		return err
	}
	return fmt.Errorf("multiple YAML documents are not supported")
}

func emptyConfig() Config {
	return Config{protectedIDs: make(map[domain.Service]map[domain.ConversationID]struct{})}
}

func newConfig(file fileConfig) (Config, error) {
	config := emptyConfig()
	for _, service := range []struct {
		name       domain.Service
		configured serviceConfig
	}{
		{name: domain.ServiceSlack, configured: file.Services.Slack},
		{name: domain.ServiceChatwork, configured: file.Services.Chatwork},
		{name: domain.ServiceZulip, configured: file.Services.Zulip},
	} {
		protected, err := protectedIDs(service.name, service.configured.ProtectedChannels)
		if err != nil {
			return Config{}, err
		}
		config.protectedIDs[service.name] = protected
	}
	return config, nil
}

func protectedIDs(service domain.Service, values []string) (map[domain.ConversationID]struct{}, error) {
	protected := make(map[domain.ConversationID]struct{}, len(values))
	for _, value := range values {
		id := strings.TrimSpace(value)
		if id == "" {
			return nil, fmt.Errorf("protected channel ID for %q must not be blank", service)
		}
		conversation, err := domain.NewConversation(service, id, "configured", domain.UnknownActivity(), domain.ProtectionNone)
		if err != nil {
			return nil, fmt.Errorf("create protected channel ID for %q: %w", service, err)
		}
		protected[conversation.ID()] = struct{}{}
	}
	return protected, nil
}

func requiredCredential(name string, lookup func(string) string) (string, error) {
	value := strings.TrimSpace(lookup(name))
	if value == "" {
		return "", fmt.Errorf("required environment variable %s is not set", name)
	}
	return value, nil
}
