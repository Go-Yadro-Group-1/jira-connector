package server

import (
	"database/sql"
	"fmt"
	"log"
	"net"
	"os"
	"os/signal"
	"syscall"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/database"
	grpchandler "github.com/Go-Yadro-Group-1/Jira-Connector/internal/handler/grpc"
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
	log        *log.Logger
}

func (s *Server) Close() {
	if s.grpcServer != nil {
		s.grpcServer.GracefulStop()
	}

	if s.db != nil {
		if err := s.db.Close(); err != nil {
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

	jiraClient := jira.New(cfg.Jira)
	repo := postgres.New(database)
	svc := sync.NewService(jiraClient, repo)
	handler := grpchandler.New(svc)

	grpcServer := grpc.NewServer()
	connectorv1.RegisterConnectorServiceServer(grpcServer, handler)

	srv := &Server{
		grpcServer: grpcServer,
		lis:        lis,
		db:         database,
		log:        log.Default(),
	}

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
