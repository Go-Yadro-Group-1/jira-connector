package sync

//go:generate mockgen -destination=mocks/mock_jira_client.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync JiraClient
//go:generate mockgen -destination=mocks/mock_repository.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository Repository

import (
	"context"
	"encoding/json"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/mapper"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/workerpool"
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

type Service struct {
	jiraClient  JiraClient
	repo        repository.Repository
	workerCount int
	queueSize   int
}

type ServiceOption func(*Service)

func WithWorkerPool(workerCount, queueSize int) ServiceOption {
	return func(s *Service) {
		s.workerCount = workerCount
		s.queueSize = queueSize
	}
}

func NewService(jiraClient JiraClient, repo repository.Repository, opts ...ServiceOption) *Service {
	svc := &Service{
		jiraClient:  jiraClient,
		repo:        repo,
		workerCount: 25,
		queueSize:   100,
	}

	for _, opt := range opts {
		opt(svc)
	}

	return svc
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

	wp := workerpool.New(s.workerCount, s.queueSize, s)
	taskCh, resultCh := wp.Run(ctx)

	go func() {
		defer close(taskCh)

		var submitted int
		startAt := 0

		for {
			resp, err := s.jiraClient.SearchIssues(ctx, jql, startAt, defaultPageSize)
			if err != nil {
				log.Printf("[workerpool] SearchIssues error: %v", err)
				return
			}

			for _, issue := range resp.Issues {
				select {
				case taskCh <- workerpool.Task{
					ID: issue.Key,
					Payload: workerpool.IssueTaskPayload{
						IssueKey:  issue.Key,
						ProjectID: projectID,
					},
				}:
					submitted++
				case <-ctx.Done():
					return
				}
			}

			if startAt+len(resp.Issues) >= resp.Total {
				break
			}

			startAt += len(resp.Issues)
		}

		log.Printf("[workerpool] Submitted %d tasks", submitted)
	}()

	var successCount, failCount int
	for result := range resultCh {
		if result.Err != nil {
			failCount++
		} else {
			successCount++
		}

		if (successCount+failCount)%50 == 0 {
			log.Printf("[workerpool] Progress: %d completed (%d ok, %d failed)", successCount+failCount, successCount, failCount)
		}
	}

	stats := wp.Stats()
	log.Printf(
		"Synced project %q: %d issues (processed=%d, failed=%d)",
		projectKey, successCount, stats.Processed.Load(), stats.Failed.Load(),
	)

	return nil
}

func (s *Service) ensureProject(ctx context.Context, projectKey string) (int64, error) {
	projectsResp, err := s.jiraClient.GetProjects(ctx, "", 100, 0)
	if err != nil {
		return 0, fmt.Errorf("get projects: %w", err)
	}

	for _, proj := range projectsResp.Values {
		if proj.Key == projectKey {
			rawProject := mapper.MapProjectToRaw(proj)
			err = s.repo.InsertProject(ctx, rawProject)
			if err != nil && err != repository.ErrProjectAlreadyExists {
				return 0, fmt.Errorf("insert project: %w", err)
			}

			return rawProject.ID, nil
		}
	}

	rawProject := mapper.MapProjectToRaw(jira.Project{Key: projectKey, Name: projectKey})
	err = s.repo.InsertProject(ctx, rawProject)
	if err != nil && err != repository.ErrProjectAlreadyExists {
		return 0, fmt.Errorf("insert project: %w", err)
	}

	return rawProject.ID, nil
}

func (s *Service) processIssue(ctx context.Context, jiraIssue jira.Issue, projectID int64) error {
	var fields jira.IssueFields
	if err := json.Unmarshal(jiraIssue.Fields, &fields); err != nil {
		return fmt.Errorf("unmarshal issue fields: %w", err)
	}

	pgRepo, ok := s.repo.(*postgres.PostgresRepository)
	if !ok {
		return fmt.Errorf("repository is not *postgres.PostgresRepository")
	}

	return pgRepo.WithTransaction(ctx, func(r *postgres.PostgresRepository) error {
		if err := s.ensureAuthorFromIssue(ctx, jiraIssue, &fields, r); err != nil {
			return fmt.Errorf("ensure author: %w", err)
		}

		rawIssue, err := mapper.MapIssueToRaw(jiraIssue, projectID, &fields)
		if err != nil {
			return fmt.Errorf("map issue to raw: %w", err)
		}

		if err := r.InsertIssue(ctx, rawIssue); err != nil {
			if err == repository.ErrIssueAlreadyExists {
				return nil
			}

			return fmt.Errorf("insert issue %s: %w", jiraIssue.Key, err)
		}

		statusChanges := mapper.MapChangelogToRaw(jiraIssue, rawIssue.ID)
		for _, change := range statusChanges {
			if err := r.InsertStatusChange(ctx, change); err != nil {
				log.Printf("[processIssue] WARNING %s: insert status change: %v", jiraIssue.Key, err)
			}
		}

		return nil
	})
}

func (s *Service) ensureAuthorFromIssue(
	ctx context.Context,
	jiraIssue jira.Issue,
	fields *jira.IssueFields,
	r *postgres.PostgresRepository,
) error {
	seen := make(map[string]bool)
	authors := []jira.Author{}

	addAuthor := func(name, displayName string) {
		if name != "" && !seen[name] {
			seen[name] = true
			authors = append(authors, jira.Author{Name: name, DisplayName: displayName})
		}
	}

	addAuthor(fields.Creator.Name, fields.Creator.DisplayName)
	addAuthor(fields.Assignee.Name, fields.Assignee.DisplayName)

	if jiraIssue.Changelog != nil {
		for _, history := range jiraIssue.Changelog.Histories {
			addAuthor(history.Author.Name, history.Author.DisplayName)
		}
	}

	for _, author := range authors {
		rawAuthor := mapper.MapAuthorToRaw(author)
		err := r.InsertAuthor(ctx, rawAuthor)
		if err != nil && err != repository.ErrAuthorAlreadyExists {
			log.Printf("Warning: failed to insert author %s: %v", author.Name, err)
		}
	}

	return nil
}

func (s *Service) Process(ctx context.Context, task workerpool.Task) error {
	payload, ok := task.Payload.(workerpool.IssueTaskPayload)
	if !ok {
		return fmt.Errorf("invalid payload type: %T", task.Payload)
	}

	jiraIssue, err := s.jiraClient.GetIssue(ctx, payload.IssueKey)
	if err != nil {
		return fmt.Errorf("get issue %s: %w", payload.IssueKey, err)
	}

	return s.processIssue(ctx, *jiraIssue, payload.ProjectID)
}
