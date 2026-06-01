// Package app is the composition root for the connector gRPC server.
// It wires database, repository, Jira client, service and handler together
// and returns a ready-to-serve *grpc.Server.
package app

import (
	"database/sql"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	grpchandler "github.com/Go-Yadro-Group-1/Jira-Connector/internal/handler/grpc"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
	syncsvc "github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
	"google.golang.org/grpc"
)

// NewGRPCServer wires the full dependency graph and returns a configured
// *grpc.Server ready to call Serve on. The db connection must already be open
// and migrations applied before calling this function.
func NewGRPCServer(db *sql.DB, jiraCfg config.JiraConfig) *grpc.Server {
	jiraClient := jira.New(jiraCfg)
	repo := postgres.New(db)
	manager := syncsvc.NewManager()
	svc := syncsvc.NewService(jiraClient, repo, manager)
	handler := grpchandler.New(svc)

	grpcServer := grpc.NewServer()
	connectorv1.RegisterConnectorServiceServer(grpcServer, handler)

	return grpcServer
}
