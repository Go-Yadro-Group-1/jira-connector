package jira_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
)

var errTestConnectionRefused = errors.New("connection refused")

func getProjectRoot() string {
	dir, err := os.Getwd()
	if err != nil {
		panic(err)
	}

	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			panic("go.mod not found")
		}
		dir = parent
	}
}

func loadMockData(filename string) (io.ReadCloser, error) {
	root := os.Getenv("PROJECT_ROOT")
	if root == "" {
		root = getProjectRoot()
	}

	path := filepath.Join(root, "internal", "client", "jira", "testdata", filename)

	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read mock data file %s: %w", path, err)
	}

	return io.NopCloser(strings.NewReader(string(data))), nil
}

func mustLoadMockData(filename string) io.ReadCloser {
	rc, err := loadMockData(filename)
	if err != nil {
		panic(err)
	}

	return rc
}

type mockTransport struct {
	roundTripFunc func(req *http.Request) (*http.Response, error)
}

func (m *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return m.roundTripFunc(req)
}

func testClient(roundTripFunc func(req *http.Request) (*http.Response, error)) *jira.Client {
	cfg := config.JiraConfig{
		BaseURL:         "https://jira.example.com",
		Token:           "test-token",
		RateLimitPerSec: 1000000,
		MinRetryDelay:   1,
		MaxRetryDelay:   5000,
		MaxResults:      50,
	}

	client := jira.New(cfg)
	client.SetTransport(&mockTransport{roundTripFunc: roundTripFunc})

	return client
}

func TestGetIssue_Success(t *testing.T) {
	t.Parallel()

	client := testClient(func(req *http.Request) (*http.Response, error) {
		if req.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", req.Method)
		}
		if !strings.Contains(req.URL.Path, "/rest/api/2/issue/TEST-123") {
			t.Errorf("unexpected path: %s", req.URL.Path)
		}
		if req.Header.Get("Authorization") != "Bearer test-token" {
			t.Errorf("expected Bearer token, got %s", req.Header.Get("Authorization"))
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("issue_success.json"),
		}, nil
	})

	issue, err := client.GetIssue(t.Context(), "TEST-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Key != "TEST-123" {
		t.Errorf("expected key TEST-123, got %s", issue.Key)
	}
	if issue.ID != "12345" {
		t.Errorf("expected id 12345, got %s", issue.ID)
	}
}

func TestGetIssue_WithChangelog(t *testing.T) {
	t.Parallel()

	client := testClient(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("issue_with_changelog.json"),
		}, nil
	})

	issue, err := client.GetIssue(t.Context(), "TEST-123")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if issue.Changelog == nil {
		t.Fatal("expected changelog, got nil")
	}
	if len(issue.Changelog.Histories) != 1 {
		t.Errorf("expected 1 history, got %d", len(issue.Changelog.Histories))
	}
}

func TestGetIssue_NotFound(t *testing.T) {
	t.Parallel()

	client := testClient(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusNotFound,
			Body:       mustLoadMockData("issue_not_found.json"),
		}, nil
	})

	_, err := client.GetIssue(t.Context(), "TEST-999")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Issue does not exist") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetIssue_Unauthorized(t *testing.T) {
	t.Parallel()

	client := testClient(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusUnauthorized,
			Body:       mustLoadMockData("empty_response.json"),
		}, nil
	})

	_, err := client.GetIssue(t.Context(), "TEST-123")

	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSearchIssues_Success(t *testing.T) {
	t.Parallel()

	client := testClient(func(req *http.Request) (*http.Response, error) {
		jql := req.URL.Query().Get("jql")
		if jql != "project=TEST" {
			t.Errorf("expected jql 'project=TEST', got %s", jql)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("search_success.json"),
		}, nil
	})

	resp, err := client.SearchIssues(t.Context(), "project=TEST", 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Issues) != 2 {
		t.Errorf("expected 2 issues, got %d", len(resp.Issues))
	}
}

func TestSearchIssues_EmptyResult(t *testing.T) {
	t.Parallel()

	client := testClient(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("search_empty.json"),
		}, nil
	})

	resp, err := client.SearchIssues(t.Context(), "project=EMPTY", 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("expected total 0, got %d", resp.Total)
	}
	if len(resp.Issues) != 0 {
		t.Errorf("expected 0 issues, got %d", len(resp.Issues))
	}
}

func TestSearchIssues_InvalidJQL(t *testing.T) {
	t.Parallel()

	client := testClient(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusBadRequest,
			Body:       mustLoadMockData("search_invalid_jql.json"),
		}, nil
	})

	_, err := client.SearchIssues(t.Context(), "invalid jql", 0, 50)

	if err == nil {
		t.Fatal("expected error, got nil")
	}
}

func TestSearchIssues_UsesDefaultMaxResults(t *testing.T) {
	t.Parallel()

	var capturedMaxResults string

	client := testClient(func(req *http.Request) (*http.Response, error) {
		capturedMaxResults = req.URL.Query().Get("maxResults")

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("search_empty.json"),
		}, nil
	})

	_, err := client.SearchIssues(t.Context(), "project=TEST", 0, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedMaxResults != "50" {
		t.Errorf("expected maxResults=50, got %s", capturedMaxResults)
	}
}

func TestGetProjects_SuccessPaginated(t *testing.T) {
	t.Parallel()

	client := testClient(func(req *http.Request) (*http.Response, error) {
		startAt := req.URL.Query().Get("startAt")
		maxResults := req.URL.Query().Get("maxResults")

		if startAt != "0" {
			t.Errorf("expected startAt=0, got %s", startAt)
		}
		if maxResults != "10" {
			t.Errorf("expected maxResults=10, got %s", maxResults)
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("projects_paginated.json"),
		}, nil
	})

	resp, err := client.GetProjects(t.Context(), "", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("expected total 2, got %d", resp.Total)
	}
	if len(resp.Values) != 2 {
		t.Errorf("expected 2 projects, got %d", len(resp.Values))
	}
	if !resp.IsLast {
		t.Error("expected isLast true, got false")
	}
}

func TestGetProjects_SimpleListResponse(t *testing.T) {
	t.Parallel()

	client := testClient(func(_ *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("projects_simple_list.json"),
		}, nil
	})

	resp, err := client.GetProjects(t.Context(), "", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(resp.Values) != 2 {
		t.Errorf("expected 2 projects, got %d", len(resp.Values))
	}
	if !resp.IsLast {
		t.Error("expected isLast true for simple list")
	}
}

func TestGetProjects_WithSearchQuery(t *testing.T) {
	t.Parallel()

	var capturedQuery string

	client := testClient(func(req *http.Request) (*http.Response, error) {
		capturedQuery = req.URL.Query().Get("query")
		if capturedQuery == "" {
			capturedQuery = req.URL.Query().Get("searchQuery")
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("projects_with_search.json"),
		}, nil
	})

	_, err := client.GetProjects(t.Context(), "test", 10, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if capturedQuery != "test" {
		t.Errorf("expected query=test, got %s", capturedQuery)
	}
}

func TestRetry_OnTooManyRequests(t *testing.T) {
	t.Parallel()

	attempts := 0

	client := testClient(func(_ *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return &http.Response{
				StatusCode: http.StatusTooManyRequests,
				Header:     http.Header{"Retry-After": []string{"1"}},
				Body:       mustLoadMockData("empty_response.json"),
			}, nil
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("search_empty.json"),
		}, nil
	})

	_, err := client.SearchIssues(t.Context(), "project=TEST", 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestRetry_OnNetworkError(t *testing.T) {
	t.Parallel()

	attempts := 0

	client := testClient(func(_ *http.Request) (*http.Response, error) {
		attempts++
		if attempts == 1 {
			return nil, errTestConnectionRefused
		}

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("search_empty.json"),
		}, nil
	})

	_, err := client.SearchIssues(t.Context(), "project=TEST", 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if attempts != 2 {
		t.Errorf("expected 2 attempts, got %d", attempts)
	}
}

func TestContextCancellation(t *testing.T) {
	t.Parallel()

	cfg := config.JiraConfig{
		BaseURL:         "https://jira.example.com",
		Token:           "test-token",
		RateLimitPerSec: 0.001,
		MinRetryDelay:   1,
		MaxRetryDelay:   10,
		MaxResults:      50,
	}

	client := jira.New(cfg)

	ctx, cancel := context.WithTimeout(t.Context(), 100*time.Millisecond)
	defer cancel()

	_, err := client.GetIssue(ctx, "TEST-123")

	if err == nil {
		t.Fatal("expected context cancellation error, got nil")
	}
	if !strings.Contains(err.Error(), "context") {
		t.Errorf("expected context error, got %v", err)
	}
}

func TestHeaders(t *testing.T) {
	t.Parallel()

	var capturedHeaders http.Header

	client := testClient(func(req *http.Request) (*http.Response, error) {
		capturedHeaders = req.Header

		return &http.Response{
			StatusCode: http.StatusOK,
			Body:       mustLoadMockData("search_empty.json"),
		}, nil
	})

	_, err := client.SearchIssues(t.Context(), "project=TEST", 0, 50)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if capturedHeaders.Get("Accept") != "application/json" {
		t.Errorf(
			"expected Accept: application/json, got %s",
			capturedHeaders.Get("Accept"),
		)
	}
	if capturedHeaders.Get("Content-Type") != "application/json" {
		t.Errorf(
			"expected Content-Type: application/json, got %s",
			capturedHeaders.Get("Content-Type"),
		)
	}
	if capturedHeaders.Get("Authorization") != "Bearer test-token" {
		t.Errorf(
			"expected Authorization: Bearer test-token, got %s",
			capturedHeaders.Get("Authorization"),
		)
	}
}

func TestError_Formatting(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		err      *jira.Error
		contains string
	}{
		{
			name:     "with message",
			err:      &jira.Error{StatusCode: 400, Message: "invalid request"},
			contains: "Jira API: 400, message: invalid request",
		},
		{
			name:     "with error messages",
			err:      &jira.Error{StatusCode: 400, ErrorMessages: []string{"field required"}},
			contains: "field required",
		},
		{
			name:     "with status only",
			err:      &jira.Error{StatusCode: 404, Body: []byte("not found")},
			contains: "Not Found",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			errStr := testCase.err.Error()
			if !strings.Contains(errStr, testCase.contains) {
				t.Errorf("Error() = %s, want to contain %s", errStr, testCase.contains)
			}
		})
	}
}
