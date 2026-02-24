package app

import (
	"context"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/broker/consumer"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/broker/publisher"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
)

type App struct {
	cfg        config.JiraConfig
	consumer   *consumer.Consumer   //nolint:unused
	publisher  *publisher.Publisher //nolint:unused
	syncer     *sync.Service        //nolint:unused
	projectKey string
}

func New(cfg config.JiraConfig, projectKey string) (*App, error) {
	jiraClient := jira.New(cfg)
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

	client := jira.New(a.cfg)

	log.Println("Fetching projects...")

	searchQuery := ""
	limit := 10
	page := 0

	projResp, err := client.GetProjects(ctx, searchQuery, limit, page)
	if err != nil {
		return fmt.Errorf("get projects failed: %w", err)
	}

	if len(projResp.Values) == 0 {
		log.Println("No projects found.")

		return nil
	}

	log.Printf("Found %d projects (Total available: %d):", len(projResp.Values), projResp.Total)
	for _, proj := range projResp.Values {
		log.Printf("- Key: [%s], Name: %s, URL: %s", proj.Key, proj.Name, proj.Self)
	}

	return nil
}
