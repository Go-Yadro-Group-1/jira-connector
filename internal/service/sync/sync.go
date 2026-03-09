package sync

import (
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
)

type Service struct {
	jiraClient *jira.Client
	repo       *postgres.PostgresRepository
}

func NewService(jiraClient *jira.Client, repo *postgres.PostgresRepository) *Service {
	return &Service{jiraClient: jiraClient, repo: repo}
}
