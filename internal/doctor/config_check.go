package doctor

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/mptooling/notifycat/internal/config"
	validationdomain "github.com/mptooling/notifycat/internal/validation/domain"
)

// CheckConfig inspects cfg and reports per-field results. Secret values are
// never written to Detail — the result reports only "set" or "missing".
func CheckConfig(cfg config.Config) Section {
	sec := Section{Name: "config"}
	sec.Checks = append(sec.Checks, secretCheck("GITHUB_WEBHOOK_SECRET", cfg.GitHubWebhookSecret))
	sec.Checks = append(sec.Checks, secretCheck("SLACK_BOT_TOKEN", cfg.SlackBotToken))

	if cfg.MessageTTLDays <= 0 {
		sec.Checks = append(sec.Checks, failResult("cleanup.message_ttl_days", "must be > 0; got %d", cfg.MessageTTLDays))
	} else {
		sec.Checks = append(sec.Checks, okResult("cleanup.message_ttl_days", fmt.Sprintf("%d days", cfg.MessageTTLDays)))
	}

	if cfg.DatabaseURL == "" {
		sec.Checks = append(sec.Checks, failResult("database.url", "missing"))
	} else {
		sec.Checks = append(sec.Checks, okResult("database.url", cfg.DatabaseURL))
	}

	if cfg.ConfigFile == "" {
		sec.Checks = append(sec.Checks, failResult("config.yaml", "missing"))
	} else {
		sec.Checks = append(sec.Checks, okResult("config.yaml", cfg.ConfigFile))
	}

	sec.Checks = append(sec.Checks, publicWebhookURLCheck(cfg.Domain))
	return sec
}

func secretCheck(name string, s config.Secret) validationdomain.CheckResult {
	if s.Reveal() == "" {
		return failResult(name, "missing; set the environment variable")
	}
	return okResult(name, "set")
}

// publicWebhookURLCheck validates server.domain and reports the exact URL the
// operator pastes into the GitHub webhook. server.domain is set in config.yaml
// under the server: block; the doctor derives https://$domain/webhook/github
// rather than asking for the URL separately. The most common install-path
// mistake is putting a scheme or path in the value, or leaving it as a bare
// host that doesn't parse — both FAIL here with a remediation hint. When
// server.domain is unset the check is a SKIP, not a FAIL: local-dev and tunnel
// (ngrok) users legitimately have no fixed public host.
func publicWebhookURLCheck(domain string) validationdomain.CheckResult {
	const name = "server.domain"
	d := strings.TrimSpace(domain)
	if d == "" {
		return skip(name, "not set — skipping the public webhook URL check (expected for local dev / tunnels; "+
			"set server.domain in config.yaml to your public host, e.g. notifycat.example.com, to enable it)")
	}
	if strings.Contains(d, "://") {
		return failResult(name, "must be a bare host like notifycat.example.com, not a full URL: got %q", d)
	}
	u, err := url.Parse("https://" + d + "/webhook/github")
	if err != nil || u.Host != d {
		return failResult(name, "not a valid host: %q — use a bare hostname like notifycat.example.com", d)
	}
	return okResult(name, "paste this into the GitHub webhook Payload URL: "+u.String())
}
