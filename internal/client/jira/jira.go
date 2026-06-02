package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/config"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/metrics"
)

const (
	defaultTimeout         = 10 * time.Minute
	defaultRateLimit       = 25
	defaultMaxResults      = 50
	defaultMinRetryDelay   = 1 * time.Second
	defaultMaxRetryDelay   = 60 * time.Second
	minAllowedResults      = 50
	maxAllowedResults      = 1000
	retryBackoffMultiplier = 2
	maxErrorBodyLength     = 200
)

var errInternalInvalidAction = errors.New("internal error: unknown action from handleResponse")

type responseAction string

const (
	actionReturn responseAction = "return"
	actionRetry  responseAction = "retry"
	actionError  responseAction = "error"
)

type Client struct {
	baseURL       string
	token         string
	client        *http.Client
	limiter       *rate.Limiter
	mtr           *metrics.Metrics
	maxResults    int
	minRetryDelay time.Duration
	maxRetryDelay time.Duration
}

type Issue struct {
	Key       string          `json:"key"`
	Self      string          `json:"self"`
	ID        string          `json:"id"`
	Fields    json.RawMessage `json:"fields"`
	Changelog *Changelog      `json:"changelog,omitempty"`
}

type IssueFields struct {
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

	Creator Author `json:"creator"`

	Assignee Author `json:"assignee"`
}

type Changelog struct {
	Histories []History `json:"histories"`
}

type History struct {
	Author  Author       `json:"author"`
	Created string       `json:"created"`
	Items   []ChangeItem `json:"items"`
}

type ChangeItem struct {
	Field      string `json:"field"`
	FromString string `json:"fromString"`
	String     string `json:"toString"`
}

type Author struct {
	Name        string `json:"name"`
	DisplayName string `json:"displayName"`
}

type Project struct {
	ID   string `json:"id"`
	Key  string `json:"key"`
	Name string `json:"name"`
	Self string `json:"self"`
}

type SearchResponse struct {
	Issues     []Issue `json:"issues"`
	Total      int     `json:"total"`
	StartAt    int     `json:"startAt"`
	MaxResults int     `json:"maxResults"`
}

type ProjectsResponse struct {
	Values     []Project `json:"values"`
	Total      int       `json:"total"`
	StartAt    int       `json:"startAt"`
	MaxResults int       `json:"maxResults"`
	IsLast     bool      `json:"isLast"`
}

type Error struct {
	StatusCode    int               `json:"-"`
	Body          []byte            `json:"-"`
	Message       string            `json:"-"`
	ErrorMessages []string          `json:"errorMessages,omitempty"`
	Errors        map[string]string `json:"errors,omitempty"`
}

func mapStatusToMessage(statusCode int) string {
	switch statusCode {
	case http.StatusUnauthorized:
		return "Unauthorized: invalid token or credentials"
	case http.StatusForbidden:
		return "Forbidden: insufficient permissions to access the resource"
	case http.StatusNotFound:
		return "Not Found: the requested resource does not exist"
	case http.StatusTooManyRequests:
		return "Rate Limit Exceeded: too many requests, please slow down"
	case http.StatusInternalServerError:
		return "Internal Server Error: Jira server encountered an unexpected condition"
	case http.StatusBadGateway:
		return "Bad Gateway: Jira server received an invalid response from upstream"
	case http.StatusServiceUnavailable:
		return "Service Unavailable: Jira server is temporarily down for maintenance"
	default:
		return "Request failed"
	}
}

func (e *Error) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("Jira API: %d, message: %s", e.StatusCode, e.Message)
	}

	if len(e.ErrorMessages) > 0 {
		return fmt.Sprintf("Jira API: %d, errors: %v", e.StatusCode, e.ErrorMessages)
	}

	if len(e.Errors) > 0 {
		return fmt.Sprintf("Jira API: %d, field errors: %v", e.StatusCode, e.Errors)
	}

	baseMsg := mapStatusToMessage(e.StatusCode)

	if len(e.Body) > 0 {
		bodyStr := string(e.Body)
		if len(bodyStr) > maxErrorBodyLength {
			bodyStr = bodyStr[:maxErrorBodyLength] + "..."
		}

		return fmt.Sprintf("%s (Status: %d). Raw body: %s", baseMsg, e.StatusCode, bodyStr)
	}

	return fmt.Sprintf("Jira API: %d, body: %s", e.StatusCode, string(e.Body))
}

func New(cfg config.JiraConfig) *Client {
	maxResults := cfg.MaxResults
	if maxResults <= 0 {
		maxResults = defaultMaxResults
	}

	if maxResults < minAllowedResults {
		maxResults = minAllowedResults
	} else if maxResults > maxAllowedResults {
		maxResults = maxAllowedResults
	}

	minRetry := time.Duration(cfg.MinRetryDelay) * time.Millisecond
	if minRetry == 0 {
		minRetry = defaultMinRetryDelay
	}

	maxRetry := time.Duration(cfg.MaxRetryDelay) * time.Millisecond
	if maxRetry == 0 {
		maxRetry = defaultMaxRetryDelay
	}

	rateLimit := cfg.RateLimitPerSec
	if rateLimit <= 0 {
		rateLimit = defaultRateLimit
	}

	return &Client{
		baseURL:       strings.TrimSuffix(cfg.BaseURL, "/"),
		token:         cfg.Token,
		maxResults:    maxResults,
		minRetryDelay: minRetry,
		maxRetryDelay: maxRetry,
		mtr:           nil,
		client: &http.Client{
			Timeout: defaultTimeout,
		},
		limiter: rate.NewLimiter(rate.Limit(rateLimit), 1),
	}
}

func (c *Client) SetTransport(transport http.RoundTripper) {
	c.client.Transport = transport
}

// SetMetrics wires Prometheus instrumentation into the client. Passing nil (the
// default) leaves the client uninstrumented.
func (c *Client) SetMetrics(mtr *metrics.Metrics) {
	c.mtr = mtr
}

// GetIssue fetches a single issue with its changelog, instrumented for metrics.
func (c *Client) GetIssue(ctx context.Context, key string) (*Issue, error) {
	start := time.Now()
	issue, err := c.fetchIssue(ctx, key)
	c.mtr.ObserveJiraRequest("get_issue", time.Since(start).Seconds(), err)

	return issue, err
}

// SearchIssues runs a JQL search, instrumented for metrics.
func (c *Client) SearchIssues(
	ctx context.Context,
	jql string,
	startAt int,
	maxResults int,
) (*SearchResponse, error) {
	start := time.Now()
	resp, err := c.searchIssues(ctx, jql, startAt, maxResults)
	c.mtr.ObserveJiraRequest("search_issues", time.Since(start).Seconds(), err)

	return resp, err
}

// GetProjects lists projects, instrumented for metrics.
func (c *Client) GetProjects(
	ctx context.Context,
	searchParam string,
	limit int,
	page int,
) (*ProjectsResponse, error) {
	start := time.Now()
	resp, err := c.fetchProjects(ctx, searchParam, limit, page)
	c.mtr.ObserveJiraRequest("get_projects", time.Since(start).Seconds(), err)

	return resp, err
}

func (c *Client) fetchIssue(ctx context.Context, key string) (*Issue, error) {
	urlStr := fmt.Sprintf("/rest/api/2/issue/%s?expand=changelog", key)

	resp, err := c.do(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch issue %q: %w", key, err)
	}
	defer resp.Body.Close()

	var issue Issue

	err = json.NewDecoder(resp.Body).Decode(&issue)
	if err != nil {
		return nil, fmt.Errorf("decode Jira issue response: %w", err)
	}

	return &issue, nil
}

func (c *Client) searchIssues(
	ctx context.Context,
	jql string,
	startAt int,
	maxResults int,
) (*SearchResponse, error) {
	if maxResults <= 0 {
		maxResults = c.maxResults
	}

	encodedJQL := url.QueryEscape(jql)
	fields := "*all"
	urlStr := fmt.Sprintf(
		"/rest/api/2/search?jql=%s&fields=%s&startAt=%d&maxResults=%d",
		encodedJQL, fields, startAt, maxResults,
	)

	resp, err := c.do(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to perform search request to %q: %w", urlStr, err)
	}
	defer resp.Body.Close()

	var searchResp SearchResponse

	err = json.NewDecoder(resp.Body).Decode(&searchResp)
	if err != nil {
		return nil, fmt.Errorf("decode Jira search response: %w", err)
	}

	return &searchResp, nil
}

func (c *Client) fetchProjects(
	ctx context.Context,
	searchParam string,
	limit int,
	page int,
) (*ProjectsResponse, error) {
	if limit <= 0 {
		limit = 50
	}

	if page < 0 {
		page = 0
	}

	startAt := page * limit

	params := url.Values{}
	params.Set("startAt", strconv.Itoa(startAt))
	params.Set("maxResults", strconv.Itoa(limit))

	if searchParam != "" {
		params.Set("searchQuery", searchParam)
	}

	urlStr := "/rest/api/2/project"

	if searchParam != "" {
		params.Set("query", searchParam)
	}

	if len(params) > 0 {
		urlStr += "?" + params.Encode()
	}

	resp, err := c.do(ctx, http.MethodGet, urlStr, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to fetch projects: %w", err)
	}
	defer resp.Body.Close()

	return parseProjectsResponse(resp)
}

func parseProjectsResponse(resp *http.Response) (*ProjectsResponse, error) {
	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response body: %w", err)
	}

	var projResp ProjectsResponse

	var simpleList []Project

	unmarshalErr := json.Unmarshal(bodyBytes, &simpleList)
	if unmarshalErr == nil {
		projResp.Values = simpleList
		projResp.Total = len(simpleList)
		projResp.IsLast = true
	} else {
		err = json.Unmarshal(bodyBytes, &projResp)
		if err != nil {
			return nil, fmt.Errorf("decode projects response: %w", err)
		}
	}

	return &projResp, nil
}

func (c *Client) handleErrorResponse(resp *http.Response) error {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("failed to read error response body: %w", err)
	}

	errAPI := &Error{
		StatusCode: resp.StatusCode,
		Body:       body,
	}

	unmarshalErr := json.Unmarshal(body, errAPI)
	if unmarshalErr != nil {
		errAPI.Message = string(body)
	}

	return errAPI
}

func (c *Client) do(
	ctx context.Context,
	method, path string,
	body io.Reader,
) (*http.Response, error) {
	var lastErr error

	delay := c.minRetryDelay

	for attempt := 0; ; attempt++ {
		err := c.limiter.Wait(ctx)
		if err != nil {
			return nil, fmt.Errorf("rate limiter wait failed: %w", err)
		}

		req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
		if err != nil {
			return nil, fmt.Errorf("create HTTP request: %w", err)
		}

		req.Header.Set("Accept", "application/json")
		req.Header.Set("Content-Type", "application/json")

		if c.token != "" {
			req.Header.Set("Authorization", "Bearer "+c.token)
		}

		resp, err := c.client.Do(req)

		action, newDelay, errResp := c.handleResponse(ctx, resp, err, delay, lastErr)

		switch action {
		case actionRetry:
			c.mtr.IncJiraRetry()

			delay = newDelay
			lastErr = errResp

			continue
		case actionError:
			return nil, errResp
		case actionReturn:
			return resp, nil
		default:
			if resp != nil {
				resp.Body.Close()
			}

			return nil, errInternalInvalidAction
		}
	}
}

func (c *Client) handleResponse(
	ctx context.Context,
	resp *http.Response,
	err error,
	delay time.Duration,
	lastErr error,
) (responseAction, time.Duration, error) {
	if err != nil {
		return c.handleNetworkError(ctx, err, lastErr, delay)
	}

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests ||
			resp.StatusCode == http.StatusServiceUnavailable {
			return c.handleRetryStatus(ctx, resp, delay)
		}

		return actionError, delay, c.handleErrorResponse(resp)
	}

	return actionReturn, delay, nil
}

func (c *Client) handleNetworkError(
	ctx context.Context,
	err error,
	lastErr error,
	delay time.Duration,
) (responseAction, time.Duration, error) {
	if ctx.Err() != nil {
		return actionError, delay, fmt.Errorf("request cancelled: %w", err)
	}

	if delay > c.maxRetryDelay {
		return actionError, delay, fmt.Errorf(
			"max retry delay exceeded on network error: %w",
			lastErr,
		)
	}

	waitErr := c.waitWithBackoff(ctx, delay)
	if waitErr != nil {
		return actionError, delay, waitErr
	}

	return actionRetry, delay * retryBackoffMultiplier, err
}

func (c *Client) handleRetryStatus(
	ctx context.Context,
	resp *http.Response,
	delay time.Duration,
) (responseAction, time.Duration, error) {
	resp.Body.Close()

	retryAfter := resp.Header.Get("Retry-After")
	if retryAfter != "" {
		sec, convErr := strconv.Atoi(retryAfter)
		if convErr == nil {
			delay = time.Duration(sec) * time.Second
		}
	}

	retryErr := c.handleErrorResponse(resp)

	if delay > c.maxRetryDelay {
		return actionError, delay, fmt.Errorf(
			"max retry delay exceeded after status %d: %w",
			resp.StatusCode,
			retryErr,
		)
	}

	waitErr := c.waitWithBackoff(ctx, delay)
	if waitErr != nil {
		return actionError, delay, waitErr
	}

	return actionRetry, delay * retryBackoffMultiplier, retryErr
}

func (c *Client) waitWithBackoff(ctx context.Context, delay time.Duration) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("context cancelled during backoff: %w", ctx.Err())
	case <-time.After(delay):
		return nil
	}
}
