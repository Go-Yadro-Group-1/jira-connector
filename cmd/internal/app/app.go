package app

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/database"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
	syncsvc "github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
)

const (
	// pollInterval is how often the CLI polls the registry while waiting for sync.
	pollInterval = 2 * time.Second
)

var (
	errJobDisappeared = errors.New("sync job disappeared from registry")
	errSyncFailed     = errors.New("sync failed")
)

// App runs a one-shot project sync from the CLI.
type App struct {
	jiraClient *jira.Client
	syncer     *syncsvc.Service
	manager    *syncsvc.Manager
	db         *sql.DB
	projectKey string
}

// New builds an App from the supplied configuration.
func New(cfg config.AppConfig, projectKey string) (*App, error) {
	jiraClient := jira.New(cfg.Jira)

	ctx := context.Background()

	dbConn, err := database.NewConnection(ctx, cfg.DB)
	if err != nil {
		return nil, fmt.Errorf("init database: %w", err)
	}

	repo := postgres.New(dbConn)
	manager := syncsvc.NewManager()
	syncer := syncsvc.NewService(jiraClient, repo, manager)

	return &App{
		jiraClient: jiraClient,
		syncer:     syncer,
		manager:    manager,
		db:         dbConn,
		projectKey: projectKey,
	}, nil
}

// Run starts the sync and blocks until it completes or the context is cancelled.
func (a *App) Run() <-chan error {
	errChan := make(chan error, 1)

	go func() {
		defer close(errChan)

		errChan <- a.run()
	}()

	return errChan
}

// Close releases the database connection.
func (a *App) Close() error {
	if a.db != nil {
		closeErr := a.db.Close()
		if closeErr != nil {
			return fmt.Errorf("close database: %w", closeErr)
		}
	}

	return nil
}

func (a *App) run() error {
	ctx := context.Background()

	log.Printf("Syncing project %q...", a.projectKey)

	result, err := a.syncer.SyncProject(ctx, a.projectKey)
	if err != nil {
		return fmt.Errorf("sync project failed: %w", err)
	}

	log.Printf("Sync started: id=%s status=%s", result.SyncID, result.Status)

	// In CLI mode we block until the background sync completes.
	return a.waitForSync(ctx, result.SyncID)
}

// waitForSync polls the job registry until the sync job reaches a terminal state.
func (a *App) waitForSync(ctx context.Context, syncID string) error {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled while waiting for sync: %w", ctx.Err())
		case <-ticker.C:
			snap, ok := a.manager.Status(syncID)
			if !ok {
				return fmt.Errorf("%w: %s", errJobDisappeared, syncID)
			}

			switch snap.State {
			case syncsvc.JobStateRunning:
				log.Printf("[sync] progress: %d/%d issues processed", snap.Processed, snap.Total)
			case syncsvc.JobStateCompleted:
				log.Printf(
					"Project %q synced successfully (%d issues)",
					a.projectKey,
					snap.Processed,
				)

				return nil
			case syncsvc.JobStateFailed:
				return fmt.Errorf("%w: %s", errSyncFailed, snap.ErrMsg)
			}
		}
	}
}
