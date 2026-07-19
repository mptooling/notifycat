package infrastructure

import (
	"fmt"

	diagnosticsdomain "github.com/mptooling/notifycat/internal/diagnostics/domain"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	routingdomain "github.com/mptooling/notifycat/internal/routing/domain"
)

// NewConfigSnapshot builds a ConfigSnapshot from a parsed Config plus the
// already-resolved routing facts. It probes the database with the same
// open+ping logic as the old doctor.CheckDatabase so the application layer
// never needs to import config or persistence.
func NewConfigSnapshot(cfg config.Config, entries []routingdomain.Entry, hasPathRules bool) diagnosticsdomain.ConfigSnapshot {
	snap := diagnosticsdomain.ConfigSnapshot{
		ConfigFile:     cfg.ConfigFile,
		DatabaseURL:    cfg.DatabaseURL,
		Domain:         cfg.Domain,
		MessageTTLDays: cfg.MessageTTLDays,

		WebhookSecretSet: cfg.ProviderWebhookSecret().Reveal() != "",
		WebhookSecretVar: cfg.ProviderWebhookSecretVar(),
		SlackTokenSet:    cfg.SlackBotToken.Reveal() != "",
		TokenSet:         cfg.ProviderToken().Reveal() != "",
		TokenVar:         cfg.ProviderTokenVar(),

		Entries:      entries,
		HasPathRules: hasPathRules,

		AIEnabled:  cfg.AI.Enabled,
		AIProvider: string(cfg.AI.Provider),
		AIModel:    cfg.AI.Model,
		AIBaseURL:  cfg.AI.BaseURL,
		AIKeySet:   cfg.AIAPIKey.Reveal() != "",
	}

	snap.DatabaseOpenable, snap.DatabaseDetail = probeDatabase(cfg.DatabaseURL)
	return snap
}

// probeDatabase opens dsn, pings the underlying connection, and returns
// (true, dsn) on success or (false, error-detail) on failure. The error
// messages are byte-identical to the old doctor.CheckDatabase output so
// existing operator runbooks stay valid.
func probeDatabase(dsn string) (openable bool, detail string) {
	if dsn == "" {
		return false, "database.url is empty; set it in config.yaml to a SQLite path or file: DSN"
	}
	db, err := persistence.Open(dsn)
	if err != nil {
		return false, fmt.Sprintf("cannot open %q: %v; ensure the parent directory exists and is writable", dsn, err)
	}
	sqlDB, err := persistence.SQLDB(db)
	if err != nil {
		return false, fmt.Sprintf("cannot resolve underlying *sql.DB: %v", err)
	}
	defer func() { _ = sqlDB.Close() }()
	if err := sqlDB.Ping(); err != nil {
		return false, fmt.Sprintf("ping failed: %v", err)
	}
	return true, dsn
}
