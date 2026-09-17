package cli

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/chck/synr/internal/adapter/chatwork"
	"github.com/chck/synr/internal/adapter/config"
	"github.com/chck/synr/internal/adapter/slack"
	"github.com/chck/synr/internal/adapter/zulip"
	"github.com/chck/synr/internal/domain"
	"github.com/chck/synr/internal/usecase"
)

const help = `Usage: synr scan --service <slack|chatwork|zulip> [--before-months N] [--apply]

Preview inactive conversations. Only --apply leaves eligible conversations.
  --service        Required service: slack, chatwork, or zulip
  --before-months   Positive number of months (default 1)
  --apply          Leave eligible conversations; stop at the first failure
  --help           Show this help without loading credentials
`

type ProviderFactory func(domain.Service, config.Credentials, *http.Client) (usecase.Provider, error)

type Dependencies struct {
	Getenv      func(string) string
	ConfigPath  string
	HTTPClient  *http.Client
	Clock       usecase.Clock
	NewProvider ProviderFactory
}

type realClock struct{}

func (realClock) Now() time.Time { return time.Now() }

func DefaultDependencies() Dependencies {
	return Dependencies{
		Getenv:      os.Getenv,
		ConfigPath:  "", // config.Load resolves the XDG/default path at use time.
		HTTPClient:  &http.Client{Timeout: 30 * time.Second},
		Clock:       realClock{},
		NewProvider: newProvider,
	}
}

func Run(ctx context.Context, args []string, stdout, stderr io.Writer, dependencies Dependencies) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return writeOutput(stdout, stderr, help)
	}
	if len(args) == 0 || args[0] != "scan" {
		fmt.Fprintln(stderr, "expected scan subcommand; run synr --help for usage")
		return 2
	}

	flags := flag.NewFlagSet("scan", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.Usage = func() {} // Run writes help to the injected stdout on ErrHelp.
	serviceName := flags.String("service", "", "service to scan")
	beforeMonths := flags.Int("before-months", 1, "positive number of months")
	apply := flags.Bool("apply", false, "leave eligible conversations")
	if err := flags.Parse(args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return writeOutput(stdout, stderr, help)
		}
		fmt.Fprintln(stderr, "run synr scan --help for usage")
		return 2
	}
	if flags.NArg() != 0 {
		fmt.Fprintln(stderr, "unexpected positional arguments; run synr scan --help for usage")
		return 2
	}
	service, err := domain.ParseService(*serviceName)
	if err != nil {
		fmt.Fprintln(stderr, "--service must be slack, chatwork, or zulip; run synr scan --help for usage")
		return 2
	}
	if *beforeMonths <= 0 {
		fmt.Fprintln(stderr, "--before-months must be a positive integer")
		return 2
	}

	configuration, err := config.Load(dependencies.ConfigPath)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	credentials, err := config.CredentialsFor(service, dependencies.Getenv)
	if err != nil {
		fmt.Fprintln(stderr, err)
		return 2
	}
	provider, err := dependencies.NewProvider(service, credentials, dependencies.HTTPClient)
	if err != nil {
		fmt.Fprintln(stderr, redact(err.Error(), credentials))
		return 2
	}

	result, scanErr := usecase.Scan(ctx, provider, usecase.Request{
		BeforeMonths: *beforeMonths,
		Apply:        *apply,
		ProtectedIDs: configuration.ProtectedIDs(service),
	}, dependencies.Clock)
	if result.Decisions != nil {
		if code := writeOutput(stdout, stderr, render(result, *apply)); code != 0 {
			return code
		}
	}
	if scanErr != nil {
		fmt.Fprintln(stderr, redact(scanErr.Error(), credentials))
		return 1
	}
	return 0
}

func newProvider(service domain.Service, credentials config.Credentials, httpClient *http.Client) (usecase.Provider, error) {
	switch service {
	case domain.ServiceSlack:
		return slack.New(credentials.Token, httpClient, "https://slack.com/api/")
	case domain.ServiceChatwork:
		return chatwork.New(credentials.Token, &url.URL{Scheme: "https", Host: "api.chatwork.com", Path: "/v2/"}, httpClient)
	case domain.ServiceZulip:
		baseURL, err := url.Parse(credentials.URL)
		if err != nil {
			return nil, fmt.Errorf("invalid SYNR_ZULIP_URL; set an absolute HTTP(S) URL")
		}
		return zulip.New(baseURL, credentials.Email, credentials.APIKey, httpClient)
	default:
		return nil, fmt.Errorf("unsupported service; select slack, chatwork, or zulip")
	}
}

func render(result usecase.Result, apply bool) string {
	var output strings.Builder
	output.WriteString("service\tname\tid\tactivity\tdecision\treason\n")
	for _, decision := range result.Decisions {
		conversation := decision.Conversation()
		activity := "unknown"
		if conversation.Activity().Known() {
			activity = conversation.Activity().At().Format(time.RFC3339)
		}
		status := "skip"
		if decision.Eligible() {
			status = "eligible"
		}
		fmt.Fprintf(&output, "%s\t%s\t%s\t%s\t%s\t%s\n", conversation.Service(), cell(conversation.Name()), cell(conversation.ID().Value()), activity, status, decision.Reason())
	}
	if apply {
		output.WriteString("result\tservice\tname\tid\n")
		for _, conversation := range result.Left {
			renderApplied(&output, "left", conversation)
		}
		if result.Failed != nil {
			renderApplied(&output, "failed", *result.Failed)
		}
		for _, conversation := range result.NotAttempted {
			renderApplied(&output, "not attempted", conversation)
		}
	}
	return output.String()
}

func renderApplied(output *strings.Builder, status string, conversation domain.Conversation) {
	fmt.Fprintf(output, "%s\t%s\t%s\t%s\n", status, conversation.Service(), cell(conversation.Name()), cell(conversation.ID().Value()))
}

func cell(value string) string {
	return strings.NewReplacer("\\", "\\\\", "\t", "\\t", "\n", "\\n", "\r", "\\r").Replace(value)
}

func redact(message string, credentials config.Credentials) string {
	for _, value := range []string{credentials.Token, credentials.APIKey, credentials.Email, credentials.URL} {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[redacted]")
		}
	}
	return message
}

func writeOutput(stdout, stderr io.Writer, output string) int {
	if _, err := io.WriteString(stdout, output); err != nil {
		fmt.Fprintf(stderr, "write output: %v; check the output destination\n", err)
		return 1
	}
	return 0
}
