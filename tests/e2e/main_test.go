//go:build e2e

package e2e_test

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/app"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/database"

	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	dbImage    = "postgres:14.5-alpine"
	dbName     = "jira_connector"
	dbUser     = "postgres"
	dbPassword = "postgres"
)

//nolint:gochecknoglobals
var (
	client   connectorv1.ConnectorServiceClient
	sharedDB *sql.DB
	// jiraServer is a stub that e2e tests can reconfigure per-scenario.
	jiraServer *httptest.Server
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

// run wires up the e2e environment and returns the test exit code. It uses
// defers for teardown so that os.Exit in TestMain runs only after cleanup.
func run(m *testing.M) int {
	ctx := context.Background()

	dbDSN, terminateDB, err := startPostgres(ctx)
	if err != nil {
		log.Printf("start postgres: %v", err)

		return 1
	}
	defer terminateDB()

	sharedDB, err = database.NewConnection(ctx, parseDSN(dbDSN))
	if err != nil {
		log.Printf("init database: %v", err)

		return 1
	}
	defer sharedDB.Close()

	// Start a shared Jira stub; individual tests override its handler as needed.
	jiraServer = httptest.NewServer(http.HandlerFunc(defaultJiraHandler))
	defer jiraServer.Close()

	grpcServer := app.NewGRPCServer(sharedDB, buildJiraConfig(jiraServer.URL))
	defer grpcServer.GracefulStop()

	lis, err := startListener(ctx, grpcServer)
	if err != nil {
		log.Printf("listen: %v", err)

		return 1
	}

	conn, err := grpc.NewClient(
		lis.Addr().String(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		log.Printf("grpc dial: %v", err)

		return 1
	}
	defer conn.Close()

	client = connectorv1.NewConnectorServiceClient(conn)

	return m.Run()
}

// buildJiraConfig returns a Jira client config pointing at the stub server.
func buildJiraConfig(baseURL string) config.JiraConfig {
	return config.JiraConfig{
		BaseURL:         baseURL,
		Token:           "",
		MaxResults:      50,
		MinRetryDelay:   1,
		MaxRetryDelay:   100,
		RateLimitPerSec: 10000,
	}
}

// startListener binds a loopback port and serves the gRPC server in the
// background, returning the listener so callers can dial it.
func startListener(ctx context.Context, grpcServer *grpc.Server) (net.Listener, error) {
	var lc net.ListenConfig

	lis, err := lc.Listen(ctx, "tcp", "127.0.0.1:0")
	if err != nil {
		return nil, fmt.Errorf("listen: %w", err)
	}

	go func() {
		serveErr := grpcServer.Serve(lis)
		if serveErr != nil {
			log.Printf("grpc serve exited: %v", serveErr)
		}
	}()

	return lis, nil
}

func startPostgres(ctx context.Context) (string, func(), error) {
	container, err := tcpostgres.Run(ctx,
		dbImage,
		tcpostgres.WithDatabase(dbName),
		tcpostgres.WithUsername(dbUser),
		tcpostgres.WithPassword(dbPassword),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		return "", nil, fmt.Errorf("run container: %w", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		_ = container.Terminate(ctx)

		return "", nil, fmt.Errorf("connection string: %w", err)
	}

	return dsn, func() { _ = container.Terminate(ctx) }, nil
}

// parseDSN converts a postgres DSN into a DBConfig using net/url for correctness.
// Expected format: postgres://user:pass@host:port/dbname?params
func parseDSN(dsn string) config.DBConfig {
	parsed, err := url.Parse(dsn)
	if err != nil {
		log.Fatalf("parseDSN: invalid DSN %q: %v", dsn, err)
	}

	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		log.Fatalf("parseDSN: invalid port in DSN %q: %v", dsn, err)
	}

	pass, _ := parsed.User.Password()

	// Strip leading slash from path to get the DB name.
	dbName := parsed.Path
	if len(dbName) > 0 && dbName[0] == '/' {
		dbName = dbName[1:]
	}

	return config.DBConfig{
		Host:     parsed.Hostname(),
		Port:     port,
		User:     parsed.User.Username(),
		Password: pass,
		DBName:   dbName,
	}
}

// defaultJiraHandler returns an empty projects list by default.
func defaultJiraHandler(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`[]`))
}

func callTimeout(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()

	return context.WithTimeout(t.Context(), 10*time.Second)
}
