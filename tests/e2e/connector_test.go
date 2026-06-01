//go:build e2e

package e2e_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// projectsListPayload is a minimal Jira projects-list response.
const projectsListPayload = `[
  {"id": "10001", "key": "TEST", "name": "Test Project", "self": "http://jira.example.com/rest/api/2/project/10001"},
  {"id": "10002", "key": "DEMO", "name": "Demo Project", "self": "http://jira.example.com/rest/api/2/project/10002"}
]`

// TestE2E_GetAvailableProjects_JiraOK verifies that GetAvailableProjects
// returns the list that the Jira stub provides.
//
//nolint:paralleltest // mutates the shared jiraServer handler.
func TestE2E_GetAvailableProjects_JiraOK(t *testing.T) {
	// Override the shared Jira stub for this test.
	jiraServer.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		rw.Header().Set("Content-Type", "application/json")
		rw.WriteHeader(http.StatusOK)
		_, _ = rw.Write([]byte(projectsListPayload))
	})

	t.Cleanup(func() {
		jiraServer.Config.Handler = http.HandlerFunc(defaultJiraHandler)
	})

	ctx, cancel := callTimeout(t)
	defer cancel()

	resp, err := client.GetAvailableProjects(ctx, &connectorv1.GetAvailableProjectsRequest{
		Limit: 10,
		Page:  0,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, int32(2), resp.GetTotal())
	assert.Len(t, resp.GetProjects(), 2)
	assert.Equal(t, "TEST", resp.GetProjects()[0].GetKey())
	assert.Equal(t, "DEMO", resp.GetProjects()[1].GetKey())
}

// TestE2E_GetAvailableProjects_JiraUnavailable verifies that when Jira returns
// 503, the connector returns a gRPC Internal error rather than panicking.
//
//nolint:paralleltest // mutates the shared jiraServer handler.
func TestE2E_GetAvailableProjects_JiraUnavailable(t *testing.T) {
	jiraServer.Config.Handler = http.HandlerFunc(func(rw http.ResponseWriter, _ *http.Request) {
		// Reply 503 without Retry-After so the client gives up quickly.
		rw.Header().Set("Content-Type", "application/json")
		http.Error(rw, `{"errorMessages":["Service Unavailable"]}`, http.StatusServiceUnavailable)
	})

	t.Cleanup(func() {
		jiraServer.Config.Handler = http.HandlerFunc(defaultJiraHandler)
	})

	ctx, cancel := callTimeout(t)
	defer cancel()

	_, err := client.GetAvailableProjects(ctx, &connectorv1.GetAvailableProjectsRequest{
		Limit: 10,
		Page:  0,
	})
	require.Error(t, err)

	code := status.Code(err)
	assert.Equal(t, codes.Internal, code, "expected Internal, got %s", code)
}

// TestE2E_DownloadProject_HappyPath verifies the full sync flow:
// - Jira stub returns 1 issue with a changelog entry;
// - DownloadProject succeeds;
// - DB rows are written to raw.project, raw.issue, raw.author, raw.status_changes.
//
//nolint:paralleltest // mutates the shared jiraServer handler and writes to the shared DB.
func TestE2E_DownloadProject_HappyPath(t *testing.T) {
	const projectKey = "E2EHAPPY"

	issuePayload := buildIssuePayload(t, "E2EHAPPY-1")
	searchPayload := fmt.Sprintf(
		`{"issues":[%s],"total":1,"startAt":0,"maxResults":50}`,
		issuePayload,
	)
	projectsPayload := fmt.Sprintf(
		`[{"id":"99999","key":%q,"name":"E2E Happy Project","self":"http://jira.example.com"}]`,
		projectKey,
	)

	jiraServer.Config.Handler = http.HandlerFunc(
		func(writer http.ResponseWriter, req *http.Request) {
			writer.Header().Set("Content-Type", "application/json")

			switch {
			case isProjectsEndpoint(req):
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(projectsPayload))
			case isSearchEndpoint(req):
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(searchPayload))
			case isIssueEndpoint(req):
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(issuePayload))
			default:
				http.NotFound(writer, req)
			}
		},
	)

	t.Cleanup(func() {
		jiraServer.Config.Handler = http.HandlerFunc(defaultJiraHandler)
	})

	ctx, cancel := callTimeout(t)
	defer cancel()

	resp, err := client.DownloadProject(ctx, &connectorv1.DownloadProjectRequest{
		ProjectKey: projectKey,
	})
	require.NoError(t, err)
	require.NotNil(t, resp)

	assert.Equal(t, "completed", resp.GetStatus())
	assert.NotEmpty(t, resp.GetProjectId())

	// Verify DB state: at least one issue row must exist for this project.
	var count int

	row := sharedDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM raw.issue i
		 JOIN raw.project p ON p.id = i.project_id
		 WHERE p.title = $1`,
		"E2E Happy Project",
	)
	require.NoError(t, row.Scan(&count))
	assert.Positive(t, count, "expected at least one issue row in DB")
}

// TestE2E_DownloadProject_JiraError verifies that when Jira returns 503 during
// sync, the gRPC call returns an error and the DB is left in a consistent state
// (the transaction was rolled back — no partial rows committed).
//
//nolint:paralleltest // mutates the shared jiraServer handler and writes to the shared DB.
func TestE2E_DownloadProject_JiraError(t *testing.T) {
	const projectKey = "E2EFAIL"

	// Projects endpoint succeeds; search fails with 503.
	projectsPayload := fmt.Sprintf(
		`[{"id":"88888","key":%q,"name":"E2E Fail Project","self":"http://jira.example.com"}]`,
		projectKey,
	)

	jiraServer.Config.Handler = http.HandlerFunc(
		func(writer http.ResponseWriter, req *http.Request) {
			writer.Header().Set("Content-Type", "application/json")

			if isProjectsEndpoint(req) {
				writer.WriteHeader(http.StatusOK)
				_, _ = writer.Write([]byte(projectsPayload))

				return
			}

			http.Error(writer, `{"errorMessages":["Jira is down"]}`, http.StatusServiceUnavailable)
		},
	)

	t.Cleanup(func() {
		jiraServer.Config.Handler = http.HandlerFunc(defaultJiraHandler)
	})

	ctx, cancel := callTimeout(t)
	defer cancel()

	_, err := client.DownloadProject(ctx, &connectorv1.DownloadProjectRequest{
		ProjectKey: projectKey,
	})
	require.Error(t, err)

	code := status.Code(err)
	assert.Equal(t, codes.Internal, code, "expected Internal, got %s", code)

	// DB must have no issue rows for this project (transaction rolled back).
	var issueCount int

	row := sharedDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM raw.issue i
		 JOIN raw.project p ON p.id = i.project_id
		 WHERE p.title = $1`,
		"E2E Fail Project",
	)
	require.NoError(t, row.Scan(&issueCount))
	assert.Equal(t, 0, issueCount, "expected no issue rows after failed sync")
}

// TestE2E_DownloadProject_EmptyProjectKey returns InvalidArgument.
func TestE2E_DownloadProject_EmptyProjectKey(t *testing.T) {
	t.Parallel()

	ctx, cancel := callTimeout(t)
	defer cancel()

	_, err := client.DownloadProject(ctx, &connectorv1.DownloadProjectRequest{
		ProjectKey: "",
	})
	require.Error(t, err)

	assert.Equal(t, codes.InvalidArgument, status.Code(err))
}

// --- helpers ---

func isProjectsEndpoint(r *http.Request) bool {
	return r.URL.Path == "/rest/api/2/project"
}

func isSearchEndpoint(r *http.Request) bool {
	return r.URL.Path == "/rest/api/2/search"
}

func isIssueEndpoint(r *http.Request) bool {
	return len(r.URL.Path) > len("/rest/api/2/issue/")
}

// jiraNamed is a Jira object that carries a display name (status, type, etc.).
type jiraNamed struct {
	Name string `json:"name"`
}

// jiraAuthor is a minimal Jira user object.
type jiraAuthor struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

// jiraChangeItem is a single field change within a changelog history entry.
type jiraChangeItem struct {
	Field      string `json:"field"`
	FromString string `json:"fromString"`
	ToString   string `json:"toString"`
}

// jiraHistory is one changelog history entry.
type jiraHistory struct {
	Author  jiraAuthor       `json:"author"`
	Created string           `json:"created"`
	Items   []jiraChangeItem `json:"items"`
}

// jiraChangelog wraps the changelog histories of an issue.
type jiraChangelog struct {
	Histories []jiraHistory `json:"histories"`
}

// jiraIssueFields holds the issue fields the mapper reads.
type jiraIssueFields struct {
	Summary   string     `json:"summary"`
	Status    jiraNamed  `json:"status"`
	IssueType jiraNamed  `json:"issuetype"`
	Priority  jiraNamed  `json:"priority"`
	Creator   jiraAuthor `json:"creator"`
	Assignee  jiraAuthor `json:"assignee"`
	Created   string     `json:"created"`
	Updated   string     `json:"updated"`
}

// jiraIssue is a minimal Jira issue with a single changelog entry.
type jiraIssue struct {
	Key       string          `json:"key"`
	ID        string          `json:"id"`
	Self      string          `json:"self"`
	Fields    jiraIssueFields `json:"fields"`
	Changelog jiraChangelog   `json:"changelog"`
}

// buildIssuePayload returns a JSON string for a minimal Jira issue with one
// changelog entry so that the mapper can produce author, issue and
// status_change rows.
func buildIssuePayload(t *testing.T, key string) string {
	t.Helper()

	iss := jiraIssue{
		Key:  key,
		ID:   "55555",
		Self: "http://jira.example.com/rest/api/2/issue/55555",
		Fields: jiraIssueFields{
			Summary:   "E2E test issue",
			Status:    jiraNamed{Name: "In Progress"},
			IssueType: jiraNamed{Name: "Story"},
			Priority:  jiraNamed{Name: "High"},
			Creator:   jiraAuthor{Name: "alice", DisplayName: "Alice"},
			Assignee:  jiraAuthor{Name: "bob", DisplayName: "Bob"},
			Created:   "2024-01-01T10:00:00.000+0000",
			Updated:   "2024-01-02T10:00:00.000+0000",
		},
		Changelog: jiraChangelog{
			Histories: []jiraHistory{
				{
					Author:  jiraAuthor{Name: "alice", DisplayName: "Alice"},
					Created: "2024-01-01T12:00:00.000+0000",
					Items: []jiraChangeItem{
						{
							Field:      "status",
							FromString: "Open",
							ToString:   "In Progress",
						},
					},
				},
			},
		},
	}

	b, err := json.Marshal(iss)
	require.NoError(t, err)

	return string(b)
}
