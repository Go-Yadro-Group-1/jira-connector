package server

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/database"
	grpchandler "github.com/Go-Yadro-Group-1/Jira-Connector/internal/handler/grpc"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/metrics"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/observability"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 50052
)

type Server struct {
	grpcServer *grpc.Server
	lis        net.Listener
	db         *sql.DB
	obs        *observability.Server
	log        *log.Logger
}

func (s *Server) Close() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}

	if s.obs != nil {
		s.obs.Shutdown()
	}

	if s.db != nil {
		err := s.db.Close()
		if err != nil {
			s.log.Printf("Failed to close database: %v", err)
		}
	}
}

//nolint:exhaustruct
func NewCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:          "serve",
		Short:        "Start the Jira Connector gRPC server",
		Long:         "Start the Jira Connector gRPC server to sync Jira project data.",
		RunE:         run,
		SilenceUsage: true,
	}

	cmd.Flags().String("host", defaultHost, "gRPC server host")
	cmd.Flags().Int("port", defaultPort, "gRPC server port")
	cmd.Flags().String("config", "", "path to config file")

	return cmd
}

func run(cmd *cobra.Command, _ []string) error {
	// Best-effort: load a local .env if present. godotenv does not override
	// variables already set in the environment, so platform-injected env
	// (e.g. Timeweb) keeps precedence; a missing file is not an error.
	_ = godotenv.Load()

	cfg, err := loadConfig(cmd)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	err = startServer(cmd, cfg)
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	return nil
}

func loadConfig(cmd *cobra.Command) (*config.AppConfig, error) {
	cfgFile, err := cmd.Flags().GetString("config")
	if err != nil {
		return nil, fmt.Errorf("get config flag: %w", err)
	}

	cfg, err := config.Load(cfgFile)
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}

	return cfg, nil
}

// buildServer wires metrics, the gRPC server, and the diagnostic endpoints into
// a ready-to-serve Server.
func buildServer(cfg *config.AppConfig, lis net.Listener, dbConn *sql.DB) *Server {
	mtr := metrics.New()
	mtr.RegisterRuntimeCollectors()

	jiraClient := jira.New(cfg.Jira)
	jiraClient.SetMetrics(mtr)

	repo := postgres.New(dbConn)
	manager := sync.NewManager()
	svc := sync.NewService(jiraClient, repo, manager, sync.WithMetrics(mtr))
	handler := grpchandler.New(svc)

	grpcServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(mtr.GRPCServer.UnaryServerInterceptor()),
	)
	connectorv1.RegisterConnectorServiceServer(grpcServer, handler)
	mtr.GRPCServer.InitializeMetrics(grpcServer)

	return &Server{
		grpcServer: grpcServer,
		lis:        lis,
		db:         dbConn,
		obs:        startObservability(cfg, mtr),
		log:        log.Default(),
	}
}

// startObservability launches the metrics and pprof endpoints enabled in cfg.
func startObservability(cfg *config.AppConfig, mtr *metrics.Metrics) *observability.Server {
	metricsAddr := ""
	if cfg.Metrics.IsEnabled() {
		metricsAddr = cfg.Metrics.Addr
	}

	pprofAddr := ""
	if cfg.Pprof.IsEnabled() {
		pprofAddr = cfg.Pprof.Addr
	}

	return observability.New(log.Default(), mtr, metricsAddr, pprofAddr)
}

func startServer(cmd *cobra.Command, cfg *config.AppConfig) error {
	host, err := cmd.Flags().GetString("host")
	if err != nil {
		return fmt.Errorf("get host flag: %w", err)
	}

	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return fmt.Errorf("get port flag: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", host, port)

	lc := net.ListenConfig{} //nolint:exhaustruct

	lis, err := lc.Listen(cmd.Context(), "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	database, err := database.NewConnection(cmd.Context(), cfg.DB)
	if err != nil {
		if lis != nil {
			_ = lis.Close()
		}

		return fmt.Errorf("init database: %w", err)
	}

	srv := buildServer(cfg, lis, database)
	grpcServer := srv.grpcServer

	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		srv.log.Println("shutting down gRPC server...")
		srv.Close()
	}()

	srv.log.Printf("gRPC server listening on %s\n", addr)

	err = grpcServer.Serve(lis)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}
