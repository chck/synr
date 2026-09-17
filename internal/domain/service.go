package domain

import "fmt"

type Service string

const (
	ServiceSlack    Service = "slack"
	ServiceChatwork Service = "chatwork"
	ServiceZulip    Service = "zulip"
)

func ParseService(value string) (Service, error) {
	service := Service(value)
	if !isSupportedService(service) {
		return "", fmt.Errorf("unsupported service %q", value)
	}
	return service, nil
}

func isSupportedService(service Service) bool {
	switch service {
	case ServiceSlack, ServiceChatwork, ServiceZulip:
		return true
	default:
		return false
	}
}
