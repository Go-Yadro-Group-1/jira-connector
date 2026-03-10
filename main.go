package main

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/repository"
	_ "github.com/lib/pq"
)

//nolint:funlen
func main() {
	connStr := "postgres://postgres:postgres@127.0.0.1:5433/postgres?sslmode=disable"

	database, err := sql.Open("postgres", connStr)
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()

	err = database.PingContext(ctx)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Connected to DB")

	repo := repository.New(database)

	project := raw.Project{
		ID:    1,
		Title: "Test Project",
	}

	err = repo.InsertProject(ctx, project)
	if err != nil {
		log.Fatal(err)
	}

	author := raw.Author{
		ID:   1,
		Name: "Danil",
	}

	err = repo.InsertAuthor(ctx, author)
	if err != nil {
		log.Fatal(err)
	}

	now := time.Now()

	issue := raw.Issue{
		ID:          1,
		ProjectID:   1,
		AuthorID:    1,
		Key:         "TEST-1",
		Summary:     strPtr("Test issue"),
		Status:      strPtr("OPEN"),
		CreatedTime: &now,
	}

	err = repo.InsertIssue(ctx, issue)
	if err != nil {
		log.Fatal(err)
	}

	change := raw.StatusChange{
		IssueID:    1,
		AuthorID:   1,
		ChangeTime: time.Now(),
		FromStatus: strPtr("OPEN"),
		ToStatus:   strPtr("IN_PROGRESS"),
	}

	err = repo.InsertStatusChange(ctx, change)
	if err != nil {
		log.Fatal(err)
	}

	log.Println("Test data inserted successfully")
	database.Close()
}

func strPtr(s string) *string {
	return &s
}
