package workerpool

import (
	"context"
	"fmt"
	"log"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
)

// SyncTaskProcessor адаптер для sync.Service чтобы использовать с worker pool.
type SyncTaskProcessor struct {
	syncService *sync.Service
}

// NewSyncTaskProcessor создает новый процессор задач синхронизации.
func NewSyncTaskProcessor(syncService *sync.Service) *SyncTaskProcessor {
	return &SyncTaskProcessor{
		syncService: syncService,
	}
}

// Process обрабатывает задачу синхронизации.
func (p *SyncTaskProcessor) Process(ctx context.Context, task Task) error {
	payload, ok := task.Payload.(IssueTaskPayload)
	if !ok {
		return fmt.Errorf("invalid payload type: expected IssueTaskPayload, got %T", task.Payload)
	}

	log.Printf("Processing issue %s for project %d", payload.IssueKey, payload.ProjectID)

	err := p.syncService.ProcessIssue(ctx, payload.IssueKey, payload.ProjectID)
	if err != nil {
		return fmt.Errorf("process issue %s: %w", payload.IssueKey, err)
	}

	return nil
}

// IssueTaskPayload содержит данные задачи для синхронизации одной задачи.
type IssueTaskPayload struct {
	IssueKey  string
	ProjectID int64
}
