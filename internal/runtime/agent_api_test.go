package runtime_test

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"
	"golang.org/x/crypto/bcrypt"
	"gorm.io/gorm"

	accessdomain "github.com/mptooling/notifycat/internal/access/domain"
	"github.com/mptooling/notifycat/internal/platform/config"
	"github.com/mptooling/notifycat/internal/platform/httpx"
	"github.com/mptooling/notifycat/internal/platform/persistence"
	"github.com/mptooling/notifycat/internal/runtime"
)

const testAdminKey = "nca_AbCdEfGhIjKlMnOpQrStUvWxYz0123456789-_abcde"

func agentAPIConfig(t *testing.T) config.Config {
	t.Helper()

	adminKeyHash, err := bcrypt.GenerateFromPassword([]byte(testAdminKey), bcrypt.MinCost)
	require.NoError(t, err)

	cfg := newTestConfig(t)
	cfg.AgentAPIEnabled = true
	cfg.AdminAddr = "127.0.0.1:9091"
	cfg.AdminKeyHash = config.Secret(adminKeyHash)
	cfg.Domain = "notifycat.example.com"
	return cfg
}

func probeRoutes() []httpx.Route {
	probe := http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		principal, _ := accessdomain.PrincipalFrom(request.Context())
		httpx.WriteJSON(w, http.StatusOK, map[string]string{"user": principal.User})
	})
	return []httpx.Route{{Pattern: "GET /api/v1/probe", Handler: probe}}
}

func buildAgentAPIApp(t *testing.T, cfg config.Config, options ...fx.Option) (*httptest.Server, *runtime.AdminServer) {
	t.Helper()

	var server *http.Server
	var adminServer *runtime.AdminServer
	var db *gorm.DB
	appOptions := append([]fx.Option{
		fx.Supply(cfg),
		runtime.Module,
		fx.NopLogger,
		fx.Provide(runtime.AsAgentAPIRoutes(probeRoutes)),
	}, options...)
	app := fx.New(append(appOptions, fx.Populate(&server, &adminServer, &db))...)
	require.NoError(t, app.Err(), "build runtime graph")
	t.Cleanup(func() {
		if sqlDB, dbErr := persistence.SQLDB(db); dbErr == nil {
			_ = sqlDB.Close()
		}
	})

	public := httptest.NewServer(server.Handler)
	t.Cleanup(public.Close)
	return public, adminServer
}

func freeLoopbackAddr(t *testing.T) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	address := listener.Addr().String()
	require.NoError(t, listener.Close())
	return address
}

func takenLoopbackAddr(t *testing.T) string {
	t.Helper()

	listener, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = listener.Close() })
	return listener.Addr().String()
}

func captureRuntimeLogger() (*slog.Logger, *bytes.Buffer) {
	logged := &bytes.Buffer{}
	return slog.New(slog.NewTextHandler(logged, nil)), logged
}

func statusOf(url string) int {
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return 0
	}
	client := &http.Client{Timeout: 5 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return 0
	}
	defer func() { _ = response.Body.Close() }()
	return response.StatusCode
}

func TestAgentAPI_DisabledServesNoRoutesAndNoAdminServer(t *testing.T) {
	cfg := agentAPIConfig(t)
	cfg.AgentAPIEnabled = false
	public, adminServer := buildAgentAPIApp(t, cfg)

	redeemStatus, _ := request(t, public, http.MethodPost, "/api/v1/invites/redeem", "", nil)
	probeStatus, _ := request(t, public, http.MethodGet, "/api/v1/probe", "",
		map[string]string{"Authorization": "Bearer nct_unknown"})

	assert.Equal(t, http.StatusNotFound, redeemStatus)
	assert.Equal(t, http.StatusNotFound, probeStatus)
	assert.Nil(t, adminServer)
}

func TestAgentAPI_EnabledRegistersRedeemAndBearerRoutes(t *testing.T) {
	public, _ := buildAgentAPIApp(t, agentAPIConfig(t))

	redeemStatus, _ := request(t, public, http.MethodPost, "/api/v1/invites/redeem", "", nil)
	probeStatus, _ := request(t, public, http.MethodGet, "/api/v1/probe", "", nil)

	assert.Equal(t, http.StatusUnauthorized, redeemStatus)
	assert.Equal(t, http.StatusUnauthorized, probeStatus)
}

func TestAgentAPI_AdminRoutesAreNotOnThePublicMux(t *testing.T) {
	testCases := []struct {
		name   string
		method string
		path   string
	}{
		{name: "create invite", method: http.MethodPost, path: "/admin/v1/invites"},
		{name: "list tokens", method: http.MethodGet, path: "/admin/v1/tokens"},
		{name: "delete token", method: http.MethodDelete, path: "/admin/v1/tokens/t1"},
	}
	public, _ := buildAgentAPIApp(t, agentAPIConfig(t))

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			status, _ := request(t, public, testCase.method, testCase.path, "",
				map[string]string{"Authorization": "Bearer " + testAdminKey})

			assert.Equal(t, http.StatusNotFound, status)
		})
	}
}

func TestAgentAPI_NoAdminServerWithoutAdminAddr(t *testing.T) {
	cfg := agentAPIConfig(t)
	cfg.AdminAddr = ""

	_, adminServer := buildAgentAPIApp(t, cfg)

	assert.Nil(t, adminServer)
}

func TestAgentAPI_AdminServerUsesHardenedTimeouts(t *testing.T) {
	_, adminServer := buildAgentAPIApp(t, agentAPIConfig(t))

	require.NotNil(t, adminServer)
	assert.Equal(t, "127.0.0.1:9091", adminServer.Addr)
	assert.Equal(t, 5*time.Second, adminServer.ReadHeaderTimeout)
	assert.Equal(t, 15*time.Second, adminServer.ReadTimeout)
	assert.Equal(t, 15*time.Second, adminServer.WriteTimeout)
	assert.Equal(t, 60*time.Second, adminServer.IdleTimeout)
	assert.Equal(t, 1<<14, adminServer.MaxHeaderBytes)
}

func TestModule_ServesAdminServerOnSecondListener(t *testing.T) {
	cfg := agentAPIConfig(t)
	cfg.Addr = "127.0.0.1:0"
	cfg.AdminAddr = freeLoopbackAddr(t)
	app := fxtest.New(t, runtime.Module, fx.Supply(cfg), fx.NopLogger)
	app.RequireStart()

	require.Eventually(t, func() bool {
		return statusOf("http://"+cfg.AdminAddr+"/admin/v1/tokens") == http.StatusUnauthorized
	}, 5*time.Second, 50*time.Millisecond)

	app.RequireStop()
}

func TestModule_AdminServerBindFailureFailsStart(t *testing.T) {
	cfg := agentAPIConfig(t)
	cfg.Addr = "127.0.0.1:0"
	cfg.AdminAddr = takenLoopbackAddr(t)
	app := fx.New(fx.Supply(cfg), runtime.Module, fx.NopLogger)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	err := app.Start(ctx)

	require.Error(t, err)
	assert.ErrorContains(t, err, "admin server")
}

func TestModule_WarnsWhenOnlyOneAdminSettingIsSet(t *testing.T) {
	testCases := []struct {
		name            string
		agentAPIEnabled bool
		adminAddr       string
		wantReason      string
	}{
		{name: "agent api without admin port", agentAPIEnabled: true, adminAddr: "", wantReason: "server.admin_addr is empty"},
		{name: "admin port without agent api", agentAPIEnabled: false, adminAddr: "127.0.0.1:9091", wantReason: "agent_api.enabled is false"},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			cfg := agentAPIConfig(t)
			cfg.AgentAPIEnabled = testCase.agentAPIEnabled
			cfg.AdminAddr = testCase.adminAddr
			logger, logged := captureRuntimeLogger()

			buildAgentAPIApp(t, cfg, fx.Replace(logger))

			assert.Contains(t, logged.String(),
				`level=WARN msg="admin server not started" reason="`+testCase.wantReason+`"`)
		})
	}
}

func TestModule_NoAdminServerWarningWhenAgentAPIAndAdminPortAreOff(t *testing.T) {
	cfg := agentAPIConfig(t)
	cfg.AgentAPIEnabled = false
	cfg.AdminAddr = ""
	logger, logged := captureRuntimeLogger()

	buildAgentAPIApp(t, cfg, fx.Replace(logger))

	assert.NotContains(t, logged.String(), "admin server not started")
}
