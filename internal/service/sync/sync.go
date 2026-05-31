package sync

//go:generate mockgen -destination=mocks/mock_jira_client.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync JiraClient
//go:generate mockgen -destination=mocks/mock_repository.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository Repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strconv"
	"time"

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
	// syncTimeout is the maximum wall-clock time allowed for a single project sync.
	// The goroutine runs on a detached context so this is the only deadline.
	syncTimeout = 4 * time.Hour
)

var (
	errInvalidPayloadType   = errors.New("invalid payload type")
	errInvalidRepository    = errors.New("repository is not *postgres.PostgresRepository")
	errUnexpectedResultType = errors.New("unexpected result type")

	// ErrProjectNotFound is returned when the project key does not exist in Jira.
	ErrProjectNotFound = errors.New("project not found in Jira")
)

// Result holds the immediate response from SyncProject.
type Result struct {
	SyncID    string
	ProjectID string
	Status    string // "running" or "already_running"
}

// JiraClient is the interface for interacting with the Jira API.
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

// IssueTaskPayload carries the data needed to fetch a single issue.
type IssueTaskPayload struct {
	IssueKey  string
	ProjectID int64
}

// Service orchestrates Jira data sync operations.
type Service struct {
	jiraClient  JiraClient
	repo        repository.Repository
	manager     *Manager
	workerCount int
	queueSize   int
}

// ServiceOption configures a Service.
type ServiceOption func(*Service)

// WithWorkerPool overrides the default worker pool sizing.
func WithWorkerPool(workerCount, queueSize int) ServiceOption {
	return func(s *Service) {
		s.workerCount = workerCount
		s.queueSize = queueSize
	}
}

// NewService creates a configured Service.
func NewService(
	jiraClient JiraClient,
	repo repository.Repository,
	manager *Manager,
	opts ...ServiceOption,
) *Service {
	svc := &Service{
		jiraClient:  jiraClient,
		repo:        repo,
		manager:     manager,
		workerCount: defaultWorkerCount,
		queueSize:   defaultQueueSize,
	}

	for _, opt := range opts {
		opt(svc)
	}

	return svc
}

// GetAvailableProjects proxies the Jira project listing.
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

// Manager returns the job registry so the handler can forward status queries.
func (s *Service) Manager() *Manager {
	return s.manager
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

// SyncProject validates the project key and starts an async sync if not already running.
// It returns immediately with a Result; the caller should poll GetSyncStatus for completion.
func (s *Service) SyncProject(ctx context.Context, projectKey string) (Result, error) {
	log.Printf("[sync] requested sync for project %q", projectKey)

	pgRepo, ok := s.repo.(*postgres.PostgresRepository)
	if !ok {
		return Result{}, errInvalidRepository
	}

	// Validate that the project key exists in Jira before registering the job.
	projectID, err := s.ensureProject(ctx, projectKey)
	if err != nil {
		return Result{}, fmt.Errorf("ensure project: %w", err)
	}

	res := s.manager.Start(projectKey)

	if !res.Started {
		log.Printf("[sync] project %q sync already running (id=%s)", projectKey, res.SyncID)

		return Result{
			SyncID:    res.SyncID,
			ProjectID: strconv.FormatInt(projectID, 10),
			Status:    "already_running",
		}, nil
	}

	syncID := res.SyncID

	// Detach from the request context so client disconnect cannot cancel the job.
	detachedCtx := context.WithoutCancel(ctx)

	go func() {
		syncCtx, cancel := context.WithTimeout(detachedCtx, syncTimeout)
		defer cancel()

		log.Printf("[sync] starting background sync id=%s project=%q", syncID, projectKey)

		syncErr := s.runSync(syncCtx, pgRepo, syncID, projectKey, projectID)
		if syncErr != nil {
			log.Printf("[sync] sync id=%s project=%q failed: %v", syncID, projectKey, syncErr)
			s.manager.Fail(syncID, syncErr.Error())

			return
		}

		log.Printf("[sync] sync id=%s project=%q completed", syncID, projectKey)
		s.manager.Complete(syncID)
	}()

	return Result{
		SyncID:    syncID,
		ProjectID: strconv.FormatInt(projectID, 10),
		Status:    "running",
	}, nil
}

// runSync fetches all issues for a project page by page, each page committed in
// its own transaction. This avoids a single long-running transaction and allows
// incremental progress even if the job fails partway through.
func (s *Service) runSync(
	ctx context.Context,
	pgRepo *postgres.PostgresRepository,
	syncID string,
	projectKey string,
	projectID int64,
) error {
	jql := "project=" + projectKey
	startAt := 0
	totalSet := false

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("sync context cancelled: %w", ctx.Err())
		default:
		}

		resp, err := s.jiraClient.SearchIssues(ctx, jql, startAt, defaultPageSize)
		if err != nil {
			return fmt.Errorf("search issues (startAt=%d): %w", startAt, err)
		}

		if !totalSet {
			s.manager.SetTotal(syncID, uint64(resp.Total)) //nolint:gosec

			totalSet = true
		}

		if len(resp.Issues) > 0 {
			pageErr := s.processPage(ctx, pgRepo, syncID, projectID, resp.Issues)
			if pageErr != nil {
				return fmt.Errorf("process page (startAt=%d): %w", startAt, pageErr)
			}
		}

		if startAt+len(resp.Issues) >= resp.Total {
			break
		}

		startAt += len(resp.Issues)
	}

	return nil
}

// processPage fetches full details for all issues in the page via the worker pool
// and then inserts them in a single per-page transaction.
func (s *Service) processPage(
	ctx context.Context,
	pgRepo *postgres.PostgresRepository,
	syncID string,
	projectID int64,
	pageIssues []jira.Issue,
) error {
	collected, err := s.fetchPageIssues(ctx, projectID, pageIssues)
	if err != nil {
		return err
	}

	return s.commitPage(ctx, pgRepo, syncID, collected)
}

// fetchPageIssues runs the worker pool for a page of issue keys and collects results.
// Per-issue fetch failures are non-fatal (logged and skipped); programming errors are fatal.
func (s *Service) fetchPageIssues(
	ctx context.Context,
	projectID int64,
	pageIssues []jira.Issue,
) ([]processedIssue, error) {
	fetcher := &issueFetcher{JiraClient: s.jiraClient}
	pool := workerpool.New(s.workerCount, s.queueSize, fetcher)
	resultCh := pool.Run(ctx)

	pageCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	group, groupCtx := errgroup.WithContext(pageCtx)

	group.Go(func() error {
		defer pool.Stop()

		for _, iss := range pageIssues {
			task := workerpool.NewTask(iss.Key, IssueTaskPayload{
				IssueKey:  iss.Key,
				ProjectID: projectID,
			})

			submitErr := pool.Submit(groupCtx, task)
			if submitErr != nil {
				return fmt.Errorf("submit %s: %w", iss.Key, submitErr)
			}
		}

		return nil
	})

	var collected []processedIssue

	group.Go(func() error {
		for res := range resultCh {
			if res.Err != nil {
				// Per-issue fetch failure is non-fatal.
				log.Printf("[sync] WARNING: fetch issue failed (task=%s): %v", res.TaskID, res.Err)

				continue
			}

			item, ok := res.Result.(processedIssue)
			if !ok {
				// Programming error: unexpected result type is fatal.
				cancel()

				return fmt.Errorf("%w: %T", errUnexpectedResultType, res.Result)
			}

			collected = append(collected, item)
		}

		return nil
	})

	waitErr := group.Wait()
	if waitErr != nil {
		return nil, fmt.Errorf("fetch page: %w", waitErr)
	}

	return collected, nil
}

// commitPage persists a batch of processed issues in a single transaction.
func (s *Service) commitPage(
	ctx context.Context,
	pgRepo *postgres.PostgresRepository,
	syncID string,
	collected []processedIssue,
) error {
	txErr := pgRepo.WithTransaction(ctx, func(txRepo *postgres.PostgresRepository) error {
		for _, item := range collected {
			insertErr := s.insertProcessedIssue(ctx, txRepo, item)
			if insertErr != nil {
				return insertErr
			}

			s.manager.IncrProcessed(syncID)
		}

		return nil
	})
	if txErr != nil {
		return fmt.Errorf("commit page: %w", txErr)
	}

	snap, ok := s.manager.Status(syncID)
	if ok && snap.Processed > 0 && snap.Processed%progressLogInterval == 0 {
		log.Printf(
			"[sync] progress id=%s: %d/%d issues processed",
			syncID,
			snap.Processed,
			snap.Total,
		)
	}

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
			log.Printf("[sync] WARNING %s: insert status change: %v", item.JiraIssue.Key, changeErr)
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
			insertErr := s.repo.InsertProject(ctx, rawProject)

			if insertErr != nil && !errors.Is(insertErr, repository.ErrProjectAlreadyExists) {
				return 0, fmt.Errorf("insert project: %w", insertErr)
			}

			return rawProject.ID, nil
		}
	}

	// Project key not found in Jira — do not fabricate a record.
	return 0, fmt.Errorf("%w: %s", ErrProjectNotFound, projectKey)
}
