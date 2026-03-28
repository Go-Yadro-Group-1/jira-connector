package postgres

import (
	"context"
	"database/sql"
	"embed"
	"errors"
	"fmt"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
	"github.com/jackc/pgx/v5/pgconn"
)

//go:embed queries/*.sql
var queriesFS embed.FS

func mustQuery(name string) string {
	b, err := queriesFS.ReadFile("queries/" + name)
	if err != nil {
		panic(err)
	}

	return string(b)
}

//nolint:gochecknoglobals
var (
	insertProjectQuery      = mustQuery("insert_project.sql")
	insertAuthorQuery       = mustQuery("insert_author.sql")
	insertIssueQuery        = mustQuery("insert_issue.sql")
	insertStatusChangeQuery = mustQuery("insert_status_change.sql")
)

//nolint:revive
type PostgresRepository struct {
	db *sql.DB
}

func New(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{
		db: db,
	}
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) {
		return pgErr.Code == "23505"
	}

	return false
}

func (r *PostgresRepository) InsertProject(ctx context.Context, project raw.Project) error {
	_, err := r.db.ExecContext(ctx, insertProjectQuery,
		project.ID,
		project.Title,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return repository.ErrProjectAlreadyExists
		}

		return fmt.Errorf("insert project: %w", err)
	}

	return nil
}

func (r *PostgresRepository) InsertAuthor(ctx context.Context, author raw.Author) error {
	_, err := r.db.ExecContext(ctx, insertAuthorQuery,
		author.ID,
		author.Name,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return repository.ErrAuthorAlreadyExists
		}

		return fmt.Errorf("insert author: %w", err)
	}

	return nil
}

func (r *PostgresRepository) InsertIssue(ctx context.Context, issue raw.Issue) error {
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
		if isUniqueViolation(err) {
			return repository.ErrIssueAlreadyExists
		}
		return fmt.Errorf("insert issue: %w", err)
	}

	return nil
}

func (r *PostgresRepository) InsertStatusChange(
	ctx context.Context,
	change raw.StatusChange,
) error {
	_, err := r.db.ExecContext(ctx, insertStatusChangeQuery,
		change.IssueID,
		change.AuthorID,
		change.ChangeTime,
		change.FromStatus,
		change.ToStatus,
	)
	if err != nil {
		return fmt.Errorf("insert status change: %w", err)
	}

	return nil
}
