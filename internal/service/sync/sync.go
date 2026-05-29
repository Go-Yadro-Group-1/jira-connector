package sync

//go:generate mockgen -destination=mocks/mock_jira_client.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync JiraClient
//go:generate mockgen -destination=mocks/mock_repository.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository Repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"sync/atomic"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/mapper"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/postgres"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/workerpool"
	"golang.org/x/sync/errgroup"
)

const (
	defaultWorkerCount   = 25
	defaultQueueSize     = 100
	defaultProjectsLimit = 100
	defaultPageSize      = 50
	progressLogInterval  = 50
)

var (
	errInvalidPayloadType   = errors.New("invalid payload type")
	errInvalidRepository    = errors.New("repository is not *postgres.PostgresRepository")
	errUnexpectedResultType = errors.New("unexpected result type")
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
	logger      *slog.Logger
	workerCount int
	queueSize   int
	processed   atomic.Uint64
}

type ServiceOption func(*Service)

func WithWorkerPool(workerCount, queueSize int) ServiceOption {
	return func(s *Service) {
		s.workerCount = workerCount
		s.queueSize = queueSize
	}
}

func WithLogger(logger *slog.Logger) ServiceOption {
	return func(s *Service) {
		s.logger = logger
	}
}

func NewService(jiraClient JiraClient, repo repository.Repository, opts ...ServiceOption) *Service {
	svc := &Service{
		jiraClient:  jiraClient,
		repo:        repo,
		logger:      slog.Default(),
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

type processedIssue struct {
	TaskID    string
	JiraIssue jira.Issue
	Fields    jira.IssueFields
	ProjectID int64
	Authors   []jira.Author
}

type issueFetcher struct {
	JiraClient JiraClient
}

func (f *issueFetcher) Process(ctx context.Context, task workerpool.Task) (any, error) {
	payload, ok := task.Payload.(IssueTaskPayload)
	if !ok {
		return nil, fmt.Errorf("%w: %T", errInvalidPayloadType, task.Payload)
	}

	issue, err := f.JiraClient.GetIssue(ctx, payload.IssueKey)
	if err != nil {
		return nil, fmt.Errorf("get issue %s: %w", payload.IssueKey, err)
	}

	var fields jira.IssueFields

	err = json.Unmarshal(issue.Fields, &fields)
	if err != nil {
		return nil, fmt.Errorf("unmarshal fields: %w", err)
	}

	authors := collectAuthors(issue, &fields)

	return processedIssue{
		TaskID:    task.ID,
		JiraIssue: *issue,
		Fields:    fields,
		ProjectID: payload.ProjectID,
		Authors:   authors,
	}, nil
}

func collectAuthors(issue *jira.Issue, fields *jira.IssueFields) []jira.Author {
	seen := make(map[string]bool)

	var authors []jira.Author

	add := func(name, displayName string) {
		if name != "" && !seen[name] {
			seen[name] = true
			authors = append(authors, jira.Author{Name: name, DisplayName: displayName})
		}
	}

	add(fields.Creator.Name, fields.Creator.DisplayName)
	add(fields.Assignee.Name, fields.Assignee.DisplayName)

	if issue.Changelog != nil {
		for _, h := range issue.Changelog.Histories {
			add(h.Author.Name, h.Author.DisplayName)
		}
	}

	return authors
}

func (s *Service) SyncProject(ctx context.Context, projectKey string) (string, error) {
	s.logger.InfoContext(ctx, "starting sync", slog.String("project_key", projectKey))

	projectID, err := s.ensureProject(ctx, projectKey)
	if err != nil {
		return "", fmt.Errorf("ensure project: %w", err)
	}

	pgRepo, ok := s.repo.(*postgres.PostgresRepository)
	if !ok {
		return "", errInvalidRepository
	}

	s.processed.Store(0)

	err = pgRepo.WithTransaction(ctx, func(txRepo *postgres.PostgresRepository) error {
		return s.syncProjectInTx(ctx, txRepo, projectKey, projectID)
	})
	if err != nil {
		return "", fmt.Errorf("sync transaction: %w", err)
	}

	return strconv.FormatInt(projectID, 10), nil
}

func (s *Service) syncProjectInTx(
	ctx context.Context,
	txRepo *postgres.PostgresRepository,
	projectKey string,
	projectID int64,
) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	fetcher := &issueFetcher{JiraClient: s.jiraClient}
	pool := workerpool.New(s.workerCount, s.queueSize, fetcher).WithLogger(s.logger)
	resultCh := pool.Run(ctx)

	errGroup, ctx := errgroup.WithContext(ctx)

	errGroup.Go(func() error {
		defer pool.Stop()

		return s.submitAllTasks(ctx, projectKey, projectID, pool)
	})

	errGroup.Go(func() error {
		return s.drainResults(ctx, cancel, txRepo, resultCh)
	})

	err := errGroup.Wait()
	if err != nil {
		return fmt.Errorf("sync project in tx: %w", err)
	}

	return nil
}

func (s *Service) drainResults(
	ctx context.Context,
	cancel context.CancelFunc,
	txRepo *postgres.PostgresRepository,
	resultCh <-chan workerpool.TaskResult,
) error {
	var firstErr error

	for res := range resultCh {
		if res.Err != nil {
			cancel()

			if firstErr == nil {
				firstErr = fmt.Errorf("task %s failed: %w", res.TaskID, res.Err)
			}

			continue
		}

		item, ok := res.Result.(processedIssue)
		if !ok {
			cancel()

			if firstErr == nil {
				firstErr = fmt.Errorf("%w: %T", errUnexpectedResultType, res.Result)
			}

			continue
		}

		insertErr := s.insertProcessedIssue(ctx, txRepo, item)
		if insertErr != nil {
			cancel()

			if firstErr == nil {
				firstErr = insertErr
			}

			continue
		}

		if count := s.processed.Add(1); count%progressLogInterval == 0 {
			s.logger.InfoContext(ctx, "sync progress", slog.Uint64("issues_processed", count))
		}
	}

	return firstErr
}

func (s *Service) submitAllTasks(
	ctx context.Context,
	projectKey string,
	projectID int64,
	pool *workerpool.WorkerPool,
) error {
	jql := "project=" + projectKey
	startAt := 0
	submitted := 0

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("submit tasks context cancelled: %w", ctx.Err())
		default:
		}

		resp, err := s.jiraClient.SearchIssues(ctx, jql, startAt, defaultPageSize)
		if err != nil {
			return fmt.Errorf("search issues: %w", err)
		}

		for _, iss := range resp.Issues {
			task := workerpool.NewTask(iss.Key, IssueTaskPayload{
				IssueKey:  iss.Key,
				ProjectID: projectID,
			})

			submitErr := pool.Submit(ctx, task)
			if submitErr != nil {
				return fmt.Errorf("submit %s: %w", iss.Key, submitErr)
			}

			submitted++
		}

		if startAt+len(resp.Issues) >= resp.Total {
			break
		}

		startAt += len(resp.Issues)
	}

	s.logger.InfoContext(
		ctx,
		"all tasks submitted",
		slog.Int("submitted", submitted),
		slog.String("project_key", projectKey),
	)

	return nil
}

func (s *Service) insertProcessedIssue(
	ctx context.Context,
	txRepo *postgres.PostgresRepository,
	item processedIssue,
) error {
	for _, a := range item.Authors {
		raw := mapper.MapAuthorToRaw(a)

		authorErr := txRepo.InsertAuthor(ctx, raw)
		if authorErr != nil && !errors.Is(authorErr, repository.ErrAuthorAlreadyExists) {
			return fmt.Errorf("author %s: %w", a.Name, authorErr)
		}
	}

	rawIssue, err := mapper.MapIssueToRaw(item.JiraIssue, item.ProjectID, &item.Fields)
	if err != nil {
		return fmt.Errorf("map issue: %w", err)
	}

	issueErr := txRepo.InsertIssue(ctx, rawIssue)
	if issueErr != nil {
		if errors.Is(issueErr, repository.ErrIssueAlreadyExists) {
			return nil
		}

		return fmt.Errorf("insert issue %s: %w", item.JiraIssue.Key, issueErr)
	}

	changes := mapper.MapChangelogToRaw(item.JiraIssue, rawIssue.ID)
	for _, ch := range changes {
		changeErr := txRepo.InsertStatusChange(ctx, ch)
		if changeErr != nil {
			s.logger.WarnContext(
				ctx,
				"insert status change failed",
				slog.String("issue_key", item.JiraIssue.Key),
				slog.Any("error", changeErr),
			)
		}
	}

	return nil
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

	if err != nil && !errors.Is(err, repository.ErrProjectAlreadyExists) {
		return 0, fmt.Errorf("insert project: %w", err)
	}

	return rawProject.ID, nil
}
