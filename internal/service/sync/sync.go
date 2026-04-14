package sync

//go:generate mockgen -destination=mocks/mock_jira_client.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync JiraClient
//go:generate mockgen -destination=mocks/mock_repository.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync Repository

import (
	"context"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/mapper"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
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
	GetIssue(
		ctx context.Context,
		key string,
	) (*jira.Issue, error)
}

type Repository interface {
	InsertProject(ctx context.Context, project raw.Project) error
	InsertAuthor(ctx context.Context, author raw.Author) error
	InsertIssue(ctx context.Context, issue raw.Issue) error
	InsertStatusChange(ctx context.Context, change raw.StatusChange) error
}

type Service struct {
	jiraClient JiraClient
	repo       Repository
}

func NewService(jiraClient JiraClient, repo Repository) *Service {
	return &Service{
		jiraClient: jiraClient,
		repo:       repo,
	}
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

	projectID, err := s.ensureProject(ctx, projectKey)
	if err != nil {
		return fmt.Errorf("ensure project: %w", err)
	}

	startAt := 0
	var totalIssues int

	for {
		resp, err := s.jiraClient.SearchIssues(ctx, jql, startAt, defaultPageSize)
		if err != nil {
			return fmt.Errorf("fetch issues: %w", err)
		}

		totalIssues = resp.Total

		for _, jiraIssue := range resp.Issues {
			if err := s.processIssue(ctx, jiraIssue, projectID); err != nil {
				return fmt.Errorf("process issue %s: %w", jiraIssue.Key, err)
			}
		}

		log.Printf(
			"Processed %d/%d issues for project %q",
			startAt+len(resp.Issues), totalIssues, projectKey,
		)

		if startAt+len(resp.Issues) >= totalIssues {
			break
		}

		startAt += len(resp.Issues)
	}

	log.Printf(
		"Successfully synced %d issues for project %q",
		totalIssues, projectKey,
	)

	return nil
}

func (s *Service) ensureProject(ctx context.Context, projectKey string) (int64, error) {
	projectsResp, err := s.jiraClient.GetProjects(ctx, "", 100, 0)
	if err != nil {
		return 0, fmt.Errorf("get projects: %w", err)
	}

	var projectID int64
	for _, proj := range projectsResp.Values {
		if proj.Key == projectKey {
			rawProject, err := mapper.MapJiraProjectToRaw(proj)
			if err != nil {
				return 0, fmt.Errorf("map project to raw: %w", err)
			}

			err = s.repo.InsertProject(ctx, rawProject)
			if err != nil && err != repository.ErrProjectAlreadyExists {
				return 0, fmt.Errorf("insert project: %w", err)
			}

			projectID = rawProject.ID
			break
		}
	}

	if projectID == 0 {
		rawProject, err := mapper.MapJiraProjectToRaw(jira.Project{
			Key:  projectKey,
			Name: projectKey,
		})
		if err != nil {
			return 0, fmt.Errorf("map project to raw: %w", err)
		}

		err = s.repo.InsertProject(ctx, rawProject)
		if err != nil && err != repository.ErrProjectAlreadyExists {
			return 0, fmt.Errorf("insert project: %w", err)
		}

		projectID = rawProject.ID
	}

	return projectID, nil
}

func (s *Service) processIssue(ctx context.Context, jiraIssue jira.Issue, projectID int64) error {
	// Сначала вставляем авторов, т.к. issue ссылается на них через FK
	// ensureAuthorFromIssue должен отработать ДО MapJiraIssueToRaw
	if err := s.ensureAuthorFromIssue(ctx, jiraIssue); err != nil {
		log.Printf("Failed to ensure author: %v", err)
	}

	rawIssue, err := mapper.MapJiraIssueToRaw(jiraIssue, projectID)
	if err != nil {
		return fmt.Errorf("map issue to raw: %w", err)
	}

	err = s.repo.InsertIssue(ctx, rawIssue)
	if err != nil && err != repository.ErrIssueAlreadyExists {
		return fmt.Errorf("insert issue: %w", err)
	}

	if jiraIssue.Changelog != nil {
		statusChanges, err := mapper.MapJiraChangelogToRaw(jiraIssue, rawIssue.ID)
		if err != nil {
			return fmt.Errorf("map changelog to raw: %w", err)
		}

		for _, change := range statusChanges {
			err = s.repo.InsertStatusChange(ctx, change)
			if err != nil {
				log.Printf("Failed to insert status change: %v", err)
			}
		}
	}

	return nil
}

// ensureAuthorFromIssue вставляет всех авторов из полей задачи и changelog.
func (s *Service) ensureAuthorFromIssue(ctx context.Context, jiraIssue jira.Issue) error {
	var fields mapper.IssueFields
	if err := mapper.ExtractFields(jiraIssue.Fields, &fields); err != nil {
		return err
	}

	seen := make(map[string]bool)
	authors := []jira.Author{}

	addAuthor := func(name, displayName string) {
		if name != "" && !seen[name] {
			seen[name] = true
			authors = append(authors, jira.Author{
				Name:        name,
				DisplayName: displayName,
			})
		}
	}

	addAuthor(fields.Creator.Name, fields.Creator.DisplayName)
	addAuthor(fields.Assignee.Name, fields.Assignee.DisplayName)

	// Добавляем авторов из changelog
	if jiraIssue.Changelog != nil {
		for _, history := range jiraIssue.Changelog.Histories {
			addAuthor(history.Author.Name, history.Author.DisplayName)
		}
	}

	for _, author := range authors {
		rawAuthor, err := mapper.MapJiraAuthorToRaw(author)
		if err != nil {
			return fmt.Errorf("map author to raw: %w", err)
		}

		err = s.repo.InsertAuthor(ctx, rawAuthor)
		if err != nil && err != repository.ErrAuthorAlreadyExists {
			log.Printf("Warning: failed to insert author %s: %v", author.Name, err)
		}
	}

	return nil
}

// ensureAuthorExists пытается вставить одного автора и возвращает его ID.
func (s *Service) ensureAuthorExists(ctx context.Context, author jira.Author) (int64, error) {
	rawAuthor, err := mapper.MapJiraAuthorToRaw(author)
	if err != nil {
		return 0, fmt.Errorf("map author to raw: %w", err)
	}

	err = s.repo.InsertAuthor(ctx, rawAuthor)
	if err != nil && err != repository.ErrAuthorAlreadyExists {
		return 0, fmt.Errorf("insert author: %w", err)
	}

	return rawAuthor.ID, nil
}

// ProcessIssue обрабатывает одну задачу - используется worker pool.
func (s *Service) ProcessIssue(ctx context.Context, issueKey string, projectID int64) error {
	jiraIssue, err := s.jiraClient.GetIssue(ctx, issueKey)
	if err != nil {
		return fmt.Errorf("get issue %s: %w", issueKey, err)
	}

	return s.processIssue(ctx, *jiraIssue, projectID)
}
