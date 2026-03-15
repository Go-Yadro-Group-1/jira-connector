package repository

import (
	"context"
	"database/sql"
	"embed"
	"fmt"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
)

//go:embed queries
var queriesFS embed.FS

func mustQuery(name string) string {
	b, err := queriesFS.ReadFile("queries/" + name)
	if err != nil {
		panic(err)
	}

	return string(b)
}

// nolint: gochecknoglobals
var (
	insertProjectQuery      = mustQuery("insert_project.sql")
	insertAuthorQuery       = mustQuery("insert_author.sql")
	insertIssueQuery        = mustQuery("insert_issue.sql")
	insertStatusChangeQuery = mustQuery("insert_status_change.sql")
)

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{
		db: db,
	}
}

func (r *Repository) InsertProject(ctx context.Context, project raw.Project) error {
	_, err := r.db.ExecContext(ctx, insertProjectQuery,
		project.ID,
		project.Title,
	)

	if err != nil {
		return fmt.Errorf("failed to insert project: %w", err)
	}

	return nil
}

func (r *Repository) InsertAuthor(ctx context.Context, author raw.Author) error {
	_, err := r.db.ExecContext(ctx, insertAuthorQuery,
		author.ID,
		author.Name,
	)

	if err != nil {
		return fmt.Errorf("failed to insert author: %w", err)
	}

	return nil
}

func (r *Repository) InsertIssue(ctx context.Context, issue raw.Issue) error {
	_, err := r.db.ExecContext(ctx, insertIssueQuery,
		issue.ID,
		issue.ProjectID,
		issue.AuthorID,
		issue.AssigneeID,
		issue.Key,
		issue.Summary,
		issue.Description,
		issue.Type,
		issue.Priority,
		issue.Status,
		issue.CreatedTime,
		issue.ClosedTime,
		issue.UpdatedTime,
		issue.TimeSpent,
	)

	if err != nil {
		return fmt.Errorf("failed to insert issue: %w", err)
	}

	return nil
}

func (r *Repository) InsertStatusChange(ctx context.Context, change raw.StatusChange) error {
	_, err := r.db.ExecContext(ctx, insertStatusChangeQuery,
		change.IssueID,
		change.AuthorID,
		change.ChangeTime,
		change.FromStatus,
		change.ToStatus,
	)

	if err != nil {
		return fmt.Errorf("failed to insert status change: %w", err)
	}

	return nil
}
