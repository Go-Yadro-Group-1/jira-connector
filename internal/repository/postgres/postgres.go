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

var (
	errNestedTransaction = errors.New(
		"cannot start nested transaction: already running in a transaction",
	)
	errUnexpectedQuerierType = errors.New(
		"unexpected querier type: expected *sql.DB to begin transaction",
	)
	errCommitOutsideTx   = errors.New("commit called outside of transaction")
	errRollbackOutsideTx = errors.New("rollback called outside of transaction")
)

type querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

//nolint:revive
type PostgresRepository struct {
	db querier
}

func New(db *sql.DB) *PostgresRepository {
	return &PostgresRepository{db: db}
}

func (r *PostgresRepository) NewTx(ctx context.Context) (*PostgresRepository, error) {
	_, isAlreadyTx := r.db.(*sql.Tx)
	if isAlreadyTx {
		return nil, errNestedTransaction
	}

	db, isDB := r.db.(*sql.DB)
	if !isDB {
		return nil, errUnexpectedQuerierType
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("begin transaction: %w", err)
	}

	return &PostgresRepository{db: tx}, nil
}

func (r *PostgresRepository) Commit() error {
	tx, isTx := r.db.(*sql.Tx)
	if !isTx {
		return errCommitOutsideTx
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}

	return nil
}

func (r *PostgresRepository) Rollback() error {
	tx, isTx := r.db.(*sql.Tx)
	if !isTx {
		return errRollbackOutsideTx
	}

	if err := tx.Rollback(); err != nil {
		return fmt.Errorf("rollback transaction: %w", err)
	}

	return nil
}

func (r *PostgresRepository) WithTransaction(
	ctx context.Context,
	operation func(*PostgresRepository) error,
) error {
	txRepo, err := r.NewTx(ctx)
	if err != nil {
		return err
	}

	execErr := operation(txRepo)
	if execErr != nil {
		_ = txRepo.Rollback()

		return execErr
	}

	return txRepo.Commit()
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
