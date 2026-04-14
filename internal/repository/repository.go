package repository

import (
	"context"
	"errors"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
)

var (
	ErrProjectAlreadyExists = errors.New("project already exists")
	ErrAuthorAlreadyExists  = errors.New("author already exists")
	ErrIssueAlreadyExists   = errors.New("issue already exists")
)

type Repository interface {
	InsertProject(ctx context.Context, project raw.Project) error
	InsertAuthor(ctx context.Context, author raw.Author) error
	InsertIssue(ctx context.Context, issue raw.Issue) error
	InsertStatusChange(ctx context.Context, change raw.StatusChange) error
}
