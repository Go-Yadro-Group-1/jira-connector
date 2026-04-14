package mapper

import (
	"encoding/json"
	"strconv"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
)

func MapProjectToRaw(proj jira.Project) raw.Project {
	return raw.Project{
		ID:    HashID(proj.Key),
		Title: proj.Name,
	}
}

func MapAuthorToRaw(author jira.Author) raw.Author {
	return raw.Author{
		ID:   HashID(author.Name),
		Name: author.DisplayName,
	}
}

func MapIssueToRaw(issue jira.Issue, projectID int64, fields *jira.IssueFields) (raw.Issue, error) {
	if err := json.Unmarshal(issue.Fields, fields); err != nil {
		return raw.Issue{}, err
	}

	id, err := strconv.ParseInt(issue.ID, 10, 64)
	if err != nil {
		return raw.Issue{}, err
	}

	rawIssue := raw.Issue{
		ID:          id,
		ProjectID:   projectID,
		AuthorID:    HashID(fields.Creator.Name),
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
		rawIssue.AssigneeID = int64Ptr(HashID(fields.Assignee.Name))
	}

	return rawIssue, nil
}

func MapChangelogToRaw(issue jira.Issue, issueID int64) []raw.StatusChange {
	if issue.Changelog == nil {
		return nil
	}

	var changes []raw.StatusChange
	for _, history := range issue.Changelog.Histories {
		changeTime, err := time.Parse(time.RFC3339, history.Created)
		if err != nil {
			continue
		}

		for _, item := range history.Items {
			if item.Field == "status" {
				changes = append(changes, raw.StatusChange{
					IssueID:    issueID,
					AuthorID:   HashID(history.Author.Name),
					ChangeTime: changeTime,
					FromStatus: strPtr(item.FromString),
					ToStatus:   strPtr(item.String),
				})
			}
		}
	}

	return changes
}

// HashID генерирует числовой ID из строки.
func HashID(s string) int64 {
	if s == "" {
		return 0
	}

	var hash int64
	for _, c := range s {
		hash = hash*31 + int64(c)
		if hash < 0 {
			hash = -hash
		}
	}

	return hash
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
