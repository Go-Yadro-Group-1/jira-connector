package sync

//go:generate mockgen -destination=mocks/mock_jira_client.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync JiraClient
//go:generate mockgen -destination=mocks/mock_repository.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository Repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/mapper"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/workerpool"
)

const (
	defaultWorkerCount   = 25
	defaultQueueSize     = 100
	defaultProjectsLimit = 100
	defaultPageSize      = 50
	progressLogInterval  = 50
)

var (
	errInvalidPayloadType = errors.New("invalid payload type")
	errInvalidRepository  = errors.New("repository is not *postgres.PostgresRepository")
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

type IssueTaskPayload struct {
	IssueKey  string
	ProjectID int64
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
		workerCount: defaultWorkerCount,
		queueSize:   defaultQueueSize,
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

func (s *Service) SyncProject(ctx context.Context, projectKey string) error {
	log.Printf("Starting sync for project %q", projectKey)

	projectID, err := s.ensureProject(ctx, projectKey)
	if err != nil {
		return fmt.Errorf("ensure project: %w", err)
	}

	return s.syncProjectIssues(ctx, projectKey, projectID)
}

func (s *Service) Process(ctx context.Context, task workerpool.Task) error {
	payload, ok := task.Payload.(IssueTaskPayload)
	if !ok {
		return fmt.Errorf("%w: %T", errInvalidPayloadType, task.Payload)
	}

	jiraIssue, err := s.jiraClient.GetIssue(ctx, payload.IssueKey)
	if err != nil {
		return fmt.Errorf("get issue %s: %w", payload.IssueKey, err)
	}

	return s.processIssue(ctx, *jiraIssue, payload.ProjectID)
}

func (s *Service) ensureProject(ctx context.Context, projectKey string) (int64, error) {
	projectsResp, err := s.jiraClient.GetProjects(ctx, "", defaultProjectsLimit, 0)
	if err != nil {
		return 0, fmt.Errorf("get projects: %w", err)
	}

	for _, proj := range projectsResp.Values {
		if proj.Key == projectKey {
			rawProject := mapper.MapProjectToRaw(proj)

			err = s.repo.InsertProject(ctx, rawProject)
			if err != nil && !errors.Is(err, repository.ErrProjectAlreadyExists) {
				return 0, fmt.Errorf("insert project: %w", err)
			}

			return rawProject.ID, nil
		}
	}

	rawProject := mapper.MapProjectToRaw(jira.Project{Key: projectKey, Name: projectKey})

	err = s.repo.InsertProject(ctx, rawProject)
	if err != nil && !errors.Is(err, repository.ErrAuthorAlreadyExists) {
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
		return errInvalidRepository
	}

	err := pgRepo.WithTransaction(ctx, func(repo *postgres.PostgresRepository) error {
		if err := s.ensureAuthorFromIssue(ctx, jiraIssue, &fields, repo); err != nil {
			return fmt.Errorf("ensure author: %w", err)
		}

		rawIssue, err := mapper.MapIssueToRaw(jiraIssue, projectID, &fields)
		if err != nil {
			return fmt.Errorf("map issue to raw: %w", err)
		}

		if err := repo.InsertIssue(ctx, rawIssue); err != nil {
			if errors.Is(err, repository.ErrIssueAlreadyExists) {
				return nil
			}

			return fmt.Errorf("insert issue %s: %w", jiraIssue.Key, err)
		}

		statusChanges := mapper.MapChangelogToRaw(jiraIssue, rawIssue.ID)
		for _, change := range statusChanges {
			if err := repo.InsertStatusChange(ctx, change); err != nil {
				log.Printf(
					"[processIssue] WARNING %s: insert status change: %v",
					jiraIssue.Key,
					err,
				)
			}
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}

	return nil
}

func (s *Service) ensureAuthorFromIssue(
	ctx context.Context,
	jiraIssue jira.Issue,
	fields *jira.IssueFields,
	repo *postgres.PostgresRepository,
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

		err := repo.InsertAuthor(ctx, rawAuthor)
		if err != nil && !errors.Is(err, repository.ErrAuthorAlreadyExists) {
			return fmt.Errorf("insert author %s: %w", author.Name, err)
		}
	}

	return nil
}

func (s *Service) syncProjectIssues(ctx context.Context, projectKey string, projectID int64) error {
	jql := "project=" + projectKey

	workerp := workerpool.New(s.workerCount, s.queueSize, s)
	resultCh := workerp.Run(ctx)

	go s.submitTasks(ctx, jql, projectID, workerp)

	successCount, failCount := s.processResults(resultCh)

	stats := workerp.Stats()
	log.Printf(
		"Synced project %q: %d issues (processed=%d, failed=%d)",
		projectKey,
		successCount+failCount,
		stats.Processed.Load(),
		stats.Failed.Load(),
	)

	return nil
}

func (s *Service) submitTasks(
	ctx context.Context,
	jql string,
	projectID int64,
	workerp *workerpool.WorkerPool,
) {
	defer workerp.Stop()

	var submitted int
	startAt := 0

	for {
		select {
		case <-ctx.Done():
			log.Printf("[workerpool] Context done, stop submitting")

			return
		default:
		}

		resp, err := s.jiraClient.SearchIssues(ctx, jql, startAt, defaultPageSize)
		if err != nil {
			log.Printf("[workerpool] SearchIssues error: %v", err)

			return
		}

		for _, issue := range resp.Issues {
			task := workerpool.Task{
				ID: issue.Key,
				Payload: IssueTaskPayload{
					IssueKey:  issue.Key,
					ProjectID: projectID,
				},
			}

			if err := workerp.Submit(ctx, task); err != nil {
				if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
					log.Printf("[workerpool] Submit canceled: %v", err)

					return
				}

				log.Printf("[workerpool] Submit error for task %s: %v", issue.Key, err)

				continue
			}
			submitted++
		}

		if startAt+len(resp.Issues) >= resp.Total {
			break
		}

		startAt += len(resp.Issues)
	}

	log.Printf("[workerpool] Submitted %d tasks", submitted)
}

func (s *Service) processResults(resultCh <-chan workerpool.TaskResult) (int, int) {
	var successCount, failCount int

	for result := range resultCh {
		if result.Err != nil {
			failCount++
		} else {
			successCount++
		}

		if (successCount+failCount)%progressLogInterval == 0 {
			log.Printf(
				"[workerpool] Progress: %d completed (%d ok, %d failed)",
				successCount+failCount,
				successCount,
				failCount,
			)
		}
	}

	return successCount, failCount
}
