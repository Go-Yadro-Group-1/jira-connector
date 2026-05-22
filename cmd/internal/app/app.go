package app

import (
	"context"
	"database/sql"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/database"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
)

type App struct {
	jiraClient *jira.Client
	syncer     *sync.Service
	db         *sql.DB
	projectKey string
}

func New(cfg config.AppConfig, projectKey string) (*App, error) {
	jiraClient := jira.New(cfg.Jira)

	ctx := context.Background()
	database, err := database.NewConnection(ctx, cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("init database: %w", err)
	}

	repo := postgres.New(database)
	syncer := sync.NewService(jiraClient, repo)

	return &App{
		jiraClient: jiraClient,
		syncer:     syncer,
		db:         database,
		projectKey: projectKey,
	}, nil
}

func (a *App) Run() <-chan error {
	errChan := make(chan error, 1)

	go func() {
		defer close(errChan)

		errChan <- a.run()
	}()

	return errChan
}

func (a *App) Close() error {
	if a.db != nil {
		if err := a.db.Close(); err != nil {
			return fmt.Errorf("close database: %w", err)
		}
	}

	return nil
}

func (a *App) run() error {
	ctx := context.Background()

	log.Printf("Syncing project %q...", a.projectKey)

	_, err := a.syncer.SyncProject(ctx, a.projectKey)
	if err != nil {
		return fmt.Errorf("sync project failed: %w", err)
	}

	log.Printf("Project %q synced successfully", a.projectKey)

	return nil
}
