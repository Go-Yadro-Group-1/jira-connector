package mapper

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/repository/models/raw"
)

const hashMultiplier = 31

func MapProjectToRaw(proj jira.Project) raw.Project {
	id, _ := strconv.ParseInt(proj.ID, 10, 64)

	return raw.Project{
		ID:    id,
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
	err := json.Unmarshal(issue.Fields, fields)
	if err != nil {
		return raw.Issue{}, fmt.Errorf("unmarshal issue fields: %w", err)
	}

	identifier, err := strconv.ParseInt(issue.ID, 10, 64)
	if err != nil {
		return raw.Issue{}, fmt.Errorf("parse issue ID %q: %w", issue.ID, err)
	}

	rawIssue := raw.Issue{
		ID:          identifier,
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

func HashID(str string) int64 {
	if str == "" {
		return 0
	}

	var hash int64
	for _, c := range str {
		hash = hash*hashMultiplier + int64(c)
		if hash < 0 {
			hash = -hash
		}
	}

	return hash
}

func strPtr(str string) *string {
	if str == "" {
		return nil
	}

	return &str
}

func intPtr(integer int) *int {
	if integer == 0 {
		return nil
	}

	return &integer
}

func int64Ptr(integer int64) *int64 {
	if integer == 0 {
		return nil
	}

	return &integer
}

func parseTime(str string) *time.Time {
	if str == "" {
		return nil
	}

	parsedTime, err := time.Parse(time.RFC3339, str)
	if err != nil {
		return nil
	}

	return &parsedTime
}
