package app

import (
	"context"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/cmd/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/broker/consumer"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/broker/publisher"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
)

type App struct {
	cfg        config.JiraConfig
	consumer   *consumer.Consumer   //nolint:unused
	publisher  *publisher.Publisher //nolint:unused
	syncer     *sync.SyncService    //nolint:unused
	projectKey string
}

func New(cfg config.JiraConfig, projectKey string) (*App, error) {
	jiraClient := jira.New(cfg.BaseURL, cfg.Token)
	// repo := postgres.NewRepository(cfg.DB.Host, cfg.DB.Port, cfg.DB.User, cfg.DB.Password, cfg.DB.DBName)

	syncer := sync.NewService(jiraClient, nil)

	return &App{
		cfg:        cfg,
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
	jql := fmt.Sprintf(`project = "%s"`, a.projectKey)

	client := jira.New(a.cfg.BaseURL, a.cfg.Token)

	log.Printf("Searching issues with JQL: %s", jql)

	resp, err := client.SearchIssues(ctx, jql)
	if err != nil {
		return fmt.Errorf("search failed: %w", err)
	}

	if len(resp.Issues) == 0 {
		log.Println("No issues found.")

		return nil
	}

	log.Printf("Found %d issues:", len(resp.Issues))
	for _, issue := range resp.Issues {
		log.Printf("- [%s] %s (Status: %s)",
			issue.Key,
			issue.Fields.Summary,
			issue.Fields.Status.Name,
		)
	}

	return nil
}
