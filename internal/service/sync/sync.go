package sync

//go:generate mockgen -destination=mocks/mock_jira_client.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync JiraClient

import (
	"context"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
)

type JiraClient interface {
	GetProjects(
		ctx context.Context,
		searchParam string,
		limit, page int,
	) (*jira.ProjectsResponse, error)
	SearchIssues(
		ctx context.Context,
		jql string,
		startAt, maxResults int,
	) (*jira.SearchResponse, error)
}

type Service struct {
	jiraClient JiraClient
}

func NewService(jiraClient JiraClient) *Service {
	return &Service{jiraClient: jiraClient}
}

func (s *Service) GetAvailableProjects(
	ctx context.Context,
	searchQuery string,
	limit, page int,
) (*jira.ProjectsResponse, error) {
	resp, err := s.jiraClient.GetProjects(ctx, searchQuery, limit, page)
	if err != nil {
		return nil, fmt.Errorf("get projects: %w", err)
	}

	return resp, nil
}

const defaultPageSize = 50

func (s *Service) SyncProject(ctx context.Context, projectKey string) error {
	log.Printf("Starting sync for project %q", projectKey)

	jql := "project=" + projectKey

	issues, err := s.jiraClient.SearchIssues(ctx, jql, 0, defaultPageSize)
	if err != nil {
		return fmt.Errorf("fetch issues: %w", err)
	}

	log.Printf(
		"Fetched %d issues (total: %d) for project %q",
		len(issues.Issues), issues.Total, projectKey,
	)

	return nil
}
