package runtime

import (
	"log/slog"
	"net/http"

	"go.uber.org/fx"

	accessdomain "github.com/mptooling/notifycat/internal/access/domain"
	accessinfra "github.com/mptooling/notifycat/internal/access/infrastructure"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/httpx"
)

// AsAgentAPIRoutes annotates a constructor returning []httpx.Route so its
// routes are served on the public mux behind Bearer token authentication.
func AsAgentAPIRoutes(constructor any) any {
	return fx.Annotate(constructor, fx.ResultTags(`group:"agent_api_routes,flatten"`))
}

// AdminServer is the admin API server on the private admin port.
type AdminServer struct {
	*http.Server
}

func provideAccessSettings(cfg config.Config) accessdomain.Settings {
	return accessdomain.Settings{
		AdminKeyHash: cfg.AdminKeyHash.Reveal(),
		PublicDomain: cfg.Domain,
	}
}

type agentAPIInputs struct {
	fx.In
	Redeemer      accessdomain.InviteRedeemer
	Authenticator accessdomain.TokenAuthenticator
	BearerRoutes  []httpx.Route `group:"agent_api_routes"`
}

// registerAgentAPI adds the invite redeem route and every Bearer-guarded agent
// route to the public mux when the agent API is enabled.
func registerAgentAPI(mux *http.ServeMux, cfg config.Config, inputs agentAPIInputs, logger *slog.Logger) {
	if !cfg.AgentAPIEnabled {
		return
	}
	mux.Handle(accessdomain.RedeemPattern, accessinfra.NewRedeemHandler(inputs.Redeemer, logger))
	requireBearer := accessinfra.BearerMiddleware(inputs.Authenticator, logger)
	for _, route := range inputs.BearerRoutes {
		mux.Handle(route.Pattern, requireBearer(route.Handler))
	}
}

type adminServerInputs struct {
	fx.In
	Config        config.Config
	Authenticator accessdomain.AdminAuthenticator
	Invites       accessdomain.InviteIssuer
	Tokens        accessdomain.TokenAdmin
	Logger        *slog.Logger
}

// buildAdminServer returns nil unless the agent API is enabled and an admin
// address is configured.
func buildAdminServer(inputs adminServerInputs) *AdminServer {
	if !inputs.Config.AgentAPIEnabled || inputs.Config.AdminAddr == "" {
		return nil
	}
	adminHandler := accessinfra.NewAdminHandler(accessdomain.AdminHandlerParams{
		Authenticator: inputs.Authenticator,
		Invites:       inputs.Invites,
		Tokens:        inputs.Tokens,
		Logger:        inputs.Logger,
	})
	return &AdminServer{Server: newHardenedServer(inputs.Config.AdminAddr, adminHandler)}
}

// registerAdminServer serves the admin server in the lifecycle, and warns when
// an admin address is set but the agent API is disabled.
func registerAdminServer(lifecycle fx.Lifecycle, adminServer *AdminServer, cfg config.Config, logger *slog.Logger, shutdowner fx.Shutdowner) {
	if adminServer == nil {
		if cfg.AdminAddr != "" {
			logger.Warn("admin server not started", slog.String("reason", "agent_api.enabled is false"))
		}
		return
	}
	serveInLifecycle(lifecycle, adminServer.Server, "admin server listening", logger, shutdowner)
}
