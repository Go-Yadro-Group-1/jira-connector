package grpchandler

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/handler/grpc Service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	syncsvc "github.com/Go-Yadro-Group-1/Jira-Connector/internal/service/sync"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Service is the narrow interface the handler requires from the service layer.
// It is defined here (consumer side) per the dependency inversion principle.
type Service interface {
	GetAvailableProjects(
		ctx context.Context,
		searchQuery string,
		limit, page int,
	) (*jira.ProjectsResponse, error)
	// SyncProject validates the project and starts an async sync.
	// Returns immediately; callers poll via GetSyncStatus.
	SyncProject(ctx context.Context, projectKey string) (syncsvc.Result, error)
	// Manager returns the job registry used by GetSyncStatus.
	Manager() *syncsvc.Manager
}

// Handler implements the ConnectorServiceServer interface.
type Handler struct {
	connectorv1.UnimplementedConnectorServiceServer

	svc Service
}

// New creates a Handler backed by svc.
func New(svc Service) *Handler {
	return &Handler{
		UnimplementedConnectorServiceServer: connectorv1.UnimplementedConnectorServiceServer{},
		svc:                                 svc,
	}
}

// LoggingInterceptor logs method, duration, and gRPC status for every unary call.
func LoggingInterceptor(logger *slog.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req any,
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (any, error) {
		start := time.Now()

		resp, err := handler(ctx, req)

		code := codes.OK
		if err != nil {
			code = status.Code(err)
		}

		logger.InfoContext(ctx, "grpc request",
			slog.String("method", info.FullMethod),
			slog.Duration("duration", time.Since(start)),
			slog.String("code", code.String()),
		)

		return resp, err
	}
}

// GetAvailableProjects lists Jira projects matching the search query.
func (h *Handler) GetAvailableProjects(
	ctx context.Context,
	req *connectorv1.GetAvailableProjectsRequest,
) (*connectorv1.GetAvailableProjectsResponse, error) {
	projResp, err := h.svc.GetAvailableProjects(
		ctx,
		req.GetSearchQuery(),
		int(req.GetLimit()),
		int(req.GetPage()),
	)
	if err != nil {
		return nil, toGRPCError(err)
	}

	projects := make([]*connectorv1.JiraProject, 0, len(projResp.Values))
	for _, proj := range projResp.Values {
		projects = append(projects, &connectorv1.JiraProject{
			Id:    proj.ID,
			Key:   proj.Key,
			Title: proj.Name,
			Self:  proj.Self,
		})
	}

	return &connectorv1.GetAvailableProjectsResponse{
		Projects: projects,
		Total:    int32(projResp.Total), //nolint:gosec
		IsLast:   projResp.IsLast,
	}, nil
}

// DownloadProject starts an async sync for the requested project and returns immediately.
// The response carries the sync_id; callers should poll GetSyncStatus for completion.
func (h *Handler) DownloadProject(
	ctx context.Context,
	req *connectorv1.DownloadProjectRequest,
) (*connectorv1.DownloadProjectResponse, error) {
	projectKey := req.GetProjectKey()
	if projectKey == "" {
		//nolint:wrapcheck // gRPC status errors must not be wrapped further
		return nil, status.Error(codes.InvalidArgument, "project_key is required")
	}

	result, err := h.svc.SyncProject(ctx, projectKey)
	if err != nil {
		return nil, toGRPCError(err)
	}

	msg := "sync started for project " + projectKey
	if result.Status == "already_running" {
		msg = "sync already in progress for project " + projectKey
	}

	return &connectorv1.DownloadProjectResponse{
		ProjectId: result.ProjectID,
		SyncId:    result.SyncID,
		Status:    result.Status,
		Message:   msg,
	}, nil
}

// GetSyncStatus returns the current state of a sync job identified by sync_id.
func (h *Handler) GetSyncStatus(
	_ context.Context,
	req *connectorv1.GetSyncStatusRequest,
) (*connectorv1.GetSyncStatusResponse, error) {
	syncID := req.GetSyncId()
	if syncID == "" {
		return nil, status.Error(codes.InvalidArgument, "sync_id is required") //nolint:wrapcheck
	}

	snap, ok := h.svc.Manager().Status(syncID)
	if !ok {
		return nil, status.Errorf(codes.NotFound, "sync job %q not found", syncID)
	}

	return &connectorv1.GetSyncStatusResponse{
		SyncId:     snap.ID,
		State:      syncStateToProto(snap.State),
		Processed:  uint32(snap.Processed), //nolint:gosec
		Total:      uint32(snap.Total),     //nolint:gosec
		Error:      snap.ErrMsg,
		ProjectKey: snap.ProjectKey,
	}, nil
}

// syncStateToProto maps internal JobState to the proto SyncState enum.
// The mapping is explicit so that adding a new internal state forces a
// deliberate proto update.
func syncStateToProto(s syncsvc.JobState) connectorv1.SyncState {
	switch s {
	case syncsvc.JobStateRunning:
		return connectorv1.SyncState_SYNC_STATE_RUNNING
	case syncsvc.JobStateCompleted:
		return connectorv1.SyncState_SYNC_STATE_COMPLETED
	case syncsvc.JobStateFailed:
		return connectorv1.SyncState_SYNC_STATE_FAILED
	default:
		return connectorv1.SyncState_SYNC_STATE_UNSPECIFIED
	}
}

func toGRPCError(err error) error {
	if errors.Is(err, context.Canceled) {
		return status.Errorf(codes.Canceled, "request cancelled: %v", err)
	}

	if errors.Is(err, syncsvc.ErrProjectNotFound) {
		return status.Errorf(codes.NotFound, "project not found: %v", err)
	}

	return status.Errorf(codes.Internal, "connector error: %v", err)
}
