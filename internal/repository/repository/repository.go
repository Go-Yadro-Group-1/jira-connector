package repository

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
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
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO raw.project (id, title)
		VALUES ($1, $2)
	`,
		project.ID,
		project.Title,
	)

	return fmt.Errorf("failed to insert project: %w", err)
}

func (r *Repository) InsertAuthor(ctx context.Context, author raw.Author) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO raw.author (id, name)
		VALUES ($1, $2)
	`,
		author.ID,
		author.Name,
	)

	return fmt.Errorf("failed to insert author: %w", err)
}

func (r *Repository) InsertIssue(ctx context.Context, issue raw.Issue) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO raw.issue (
			id,
			project_id,
			author_id,
			assignee_id,
			key,
			summary,
			description,
			type,
			priority,
			status,
			created_time,
			closed_time,
			updated_time,
			time_spent
		)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14
		)
	`,
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

	return fmt.Errorf("failed to insert issue: %w", err)
}

func (r *Repository) InsertStatusChange(ctx context.Context, change raw.StatusChange) error {
	_, err := r.db.ExecContext(ctx, `
		INSERT INTO raw.status_changes (
			issue_id,
			author_id,
			change_time,
			from_status,
			to_status
		)
		VALUES ($1,$2,$3,$4,$5)
	`,
		change.IssueID,
		change.AuthorID,
		change.ChangeTime,
		change.FromStatus,
		change.ToStatus,
	)

	return fmt.Errorf("failed to insert status change: %w", err)
}
