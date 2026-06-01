package server

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/joho/godotenv"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/database"
	grpchandler "github.com/Go-Yadro-Group-1/Jira-Connector/internal/handler/grpc"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
	syncsvc "github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
)

const (
	defaultHost = "0.0.0.0"
	defaultPort = 50052
)

type Server struct {
	grpcServer  *grpc.Server
	lis         net.Listener
	db          *sql.DB
	pprofServer *http.Server
	logger      *slog.Logger
}

func (s *Server) Close() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}

	if s.pprofServer != nil {
		pprofShutdown(s.pprofServer)
	}

	if s.db != nil {
		err := s.db.Close()
		if err != nil {
			s.logger.Error("failed to close database", slog.Any("error", err))
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

	logger := buildLogger(cfg.App.LogLevel)

	err = startServer(cmd, cfg, logger)
	if err != nil {
		return fmt.Errorf("start server: %w", err)
	}

	return nil
}

func buildLogger(levelStr string) *slog.Logger {
	var level slog.Level

	err := level.UnmarshalText([]byte(levelStr))
	if err != nil {
		level = slog.LevelInfo
	}

	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: level})

	return slog.New(handler)
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

func startServer(cmd *cobra.Command, cfg *config.AppConfig, logger *slog.Logger) error {
	addr, err := resolveListenAddr(cmd)
	if err != nil {
		return err
	}

	lc := net.ListenConfig{} //nolint:exhaustruct

	lis, err := lc.Listen(cmd.Context(), "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}

	dbConn, err := database.NewConnection(cmd.Context(), cfg.DB)
	if err != nil {
		if lis != nil {
			_ = lis.Close()
		}

		return fmt.Errorf("init database: %w", err)
	}

	grpcServer := buildGRPCServer(dbConn, cfg, logger)

	var pprofSrv *http.Server
	if cfg.Pprof.Enabled {
		pprofSrv = startPprofServer(cfg.Pprof.Addr, logger)
	}

	srv := &Server{
		grpcServer:  grpcServer,
		lis:         lis,
		db:          dbConn,
		pprofServer: pprofSrv,
		logger:      logger,
	}

	registerShutdownHook(srv, logger)

	logger.Info("gRPC server listening", slog.String("addr", addr))

	err = grpcServer.Serve(lis)
	if err != nil {
		return fmt.Errorf("serve: %w", err)
	}

	return nil
}

func resolveListenAddr(cmd *cobra.Command) (string, error) {
	host, err := cmd.Flags().GetString("host")
	if err != nil {
		return "", fmt.Errorf("get host flag: %w", err)
	}

	port, err := cmd.Flags().GetInt("port")
	if err != nil {
		return "", fmt.Errorf("get port flag: %w", err)
	}

	return fmt.Sprintf("%s:%d", host, port), nil
}

func buildGRPCServer(dbConn *sql.DB, cfg *config.AppConfig, logger *slog.Logger) *grpc.Server {
	jiraClient := jira.New(cfg.Jira)
	repo := postgres.New(dbConn)
	svc := syncsvc.NewService(jiraClient, repo, syncsvc.WithLogger(logger))
	handler := grpchandler.New(svc, logger)

	grpcServer := grpc.NewServer(grpc.ChainUnaryInterceptor(
		grpchandler.LoggingInterceptor(logger),
	))
	connectorv1.RegisterConnectorServiceServer(grpcServer, handler)

	return grpcServer
}

func registerShutdownHook(srv *Server, logger *slog.Logger) {
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		<-quit
		logger.Info("shutting down server")
		srv.Close()
	}()
}
