package app

import (
	"context"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
)

type App struct {
	jiraClient *jira.Client
	syncer     *sync.Service
	projectKey string
}

func New(cfg config.JiraConfig, projectKey string) (*App, error) {
	jiraClient := jira.New(cfg)
	syncer := sync.NewService(jiraClient)

	return &App{
		jiraClient: jiraClient,
		syncer:     syncer,
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
	return nil
}

func (a *App) run() error {
	ctx := context.Background()

	log.Printf("Syncing project %q...", a.projectKey)

	err := a.syncer.SyncProject(ctx, a.projectKey, false)
	if err != nil {
		return fmt.Errorf("sync project failed: %w", err)
	}

	log.Printf("Project %q synced successfully", a.projectKey)

	return nil
}
