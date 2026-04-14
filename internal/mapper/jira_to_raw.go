package mapper

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
)

// MapJiraProjectToRaw преобразует Jira проект в raw.Project.
// ID генерируется из хэша ключа проекта.
func MapJiraProjectToRaw(proj jira.Project) (raw.Project, error) {
	id, err := generateIDFromString(proj.Key)
	if err != nil {
		return raw.Project{}, fmt.Errorf("generate project ID: %w", err)
	}

	return raw.Project{
		ID:    id,
		Title: proj.Name,
	}, nil
}

// MapJiraAuthorToRaw преобразует Jira автора в raw.Author.
// ID генерируется из имени автора.
func MapJiraAuthorToRaw(author jira.Author) (raw.Author, error) {
	id, err := generateIDFromString(author.Name)
	if err != nil {
		return raw.Author{}, fmt.Errorf("generate author ID: %w", err)
	}

	return raw.Author{
		ID:   id,
		Name: author.DisplayName,
	}, nil
}

// MapJiraIssueToRaw преобразует Jira задачу в raw.Issue.
// Извлекает поля из JSON и маппит на структуру БД.
func MapJiraIssueToRaw(issue jira.Issue, projectID int64) (raw.Issue, error) {
	var fields issueFields
	if err := json.Unmarshal(issue.Fields, &fields); err != nil {
		return raw.Issue{}, fmt.Errorf("unmarshal issue fields: %w", err)
	}

	id, err := strconv.ParseInt(issue.ID, 10, 64)
	if err != nil {
		return raw.Issue{}, fmt.Errorf("parse issue ID: %w", err)
	}

	authorID, err := generateIDFromString(fields.Creator.Name)
	if err != nil {
		return raw.Issue{}, fmt.Errorf("generate author ID: %w", err)
	}

	rawIssue := raw.Issue{
		ID:          id,
		ProjectID:   projectID,
		AuthorID:    authorID,
		Key:         issue.Key,
		Summary:     strPtr(fields.Summary),
		Description: strPtr(fields.Description),
		Type:        strPtr(fields.IssueType.Name),
		Priority:    strPtr(fields.Priority.Name),
		Status:      strPtr(fields.Status.Name),
		CreatedTime: parseTime(fields.Created),
		UpdatedTime: parseTime(fields.Updated),
		ClosedTime:  parseTime(fields.Resolutiondate),
		TimeSpent:   intPtr(fields.TimeSpent),
	}

	if fields.Assignee.Name != "" {
		assigneeID, err := generateIDFromString(fields.Assignee.Name)
		if err != nil {
			return raw.Issue{}, fmt.Errorf("generate assignee ID: %w", err)
		}

		rawIssue.AssigneeID = int64Ptr(assigneeID)
	}

	return rawIssue, nil
}

// MapJiraChangelogToRaw преобразует Jira changelog в список изменений статусов.
func MapJiraChangelogToRaw(issue jira.Issue, issueID int64) ([]raw.StatusChange, error) {
	if issue.Changelog == nil {
		return nil, nil
	}

	var changes []raw.StatusChange

	for _, history := range issue.Changelog.Histories {
		changeTime, err := time.Parse(time.RFC3339, history.Created)
		if err != nil {
			continue
		}

		authorID, err := generateIDFromString(history.Author.Name)
		if err != nil {
			return nil, fmt.Errorf("generate author ID: %w", err)
		}

		for _, item := range history.Items {
			if item.Field == "status" {
				changes = append(changes, raw.StatusChange{
					IssueID:    issueID,
					AuthorID:   authorID,
					ChangeTime: changeTime,
					FromStatus: strPtr(item.FromString),
					ToStatus:   strPtr(item.String),
				})
			}
		}
	}

	return changes, nil
}

// issueFields представляет структуру полей Jira задачи.
type issueFields struct {
	Summary        string `json:"summary"`
	Description    string `json:"description"`
	Created        string `json:"created"`
	Updated        string `json:"updated"`
	Resolutiondate string `json:"resolutiondate"`
	TimeSpent      int    `json:"timespent"`

	IssueType struct {
		Name string `json:"name"`
	} `json:"issuetype"`

	Priority struct {
		Name string `json:"name"`
	} `json:"priority"`

	Status struct {
		Name string `json:"name"`
	} `json:"status"`

	Creator struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"creator"`

	Assignee struct {
		Name        string `json:"name"`
		DisplayName string `json:"displayName"`
	} `json:"assignee"`
}

// IssueFields - экспортированная версия для использования в сервисе.
type IssueFields = issueFields

// ExtractFields извлекает поля из JSON задачи в структуру IssueFields.
func ExtractFields(fields json.RawMessage, target *IssueFields) error {
	return json.Unmarshal(fields, target)
}

// generateIDFromString генерирует числовой ID из строки используя простой хэш.
func generateIDFromString(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}

	var hash int64
	for _, c := range s {
		hash = hash*31 + int64(c)
		if hash < 0 {
			hash = -hash
		}
	}

	return hash, nil
}

func strPtr(s string) *string {
	if s == "" {
		return nil
	}

	return &s
}

func intPtr(i int) *int {
	if i == 0 {
		return nil
	}

	return &i
}

func int64Ptr(i int64) *int64 {
	if i == 0 {
		return nil
	}

	return &i
}

func parseTime(s string) *time.Time {
	if s == "" {
		return nil
	}

	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return nil
	}

	return &t
}
