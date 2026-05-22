package grpchandler

//go:generate mockgen -destination=mocks/mock_service.go -package=mocks github.com/Go-Yadro-Group-1/Jira-Connector/internal/handler/grpc Service

import (
	"context"
	"fmt"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"github.com/Go-Yadro-Group-1/Jira-Connector/internal/client/jira"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type Service interface {
	GetAvailableProjects(
		ctx context.Context,
		searchQuery string,
		limit, page int,
	) (*jira.ProjectsResponse, error)
	SyncProject(ctx context.Context, projectKey string) (string, error)
}

type Handler struct {
	connectorv1.UnimplementedConnectorServiceServer

	svc Service
}

func New(svc Service) *Handler {
	return &Handler{
		UnimplementedConnectorServiceServer: connectorv1.UnimplementedConnectorServiceServer{},
		svc:                                 svc,
	}
}

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

func (h *Handler) DownloadProject(
	ctx context.Context,
	req *connectorv1.DownloadProjectRequest,
) (*connectorv1.DownloadProjectResponse, error) {
	projectKey := req.GetProjectKey()
	if projectKey == "" {
		return nil, fmt.Errorf(
			"validate request: %w",
			status.Error(codes.InvalidArgument, "project_key is required"),
		)
	}

	projectID, err := h.svc.SyncProject(ctx, projectKey)
	if err != nil {
		return nil, toGRPCError(err)
	}

	return &connectorv1.DownloadProjectResponse{
		ProjectId: projectID,
		SyncId:    projectKey,
		Status:    "completed",
		Message:   fmt.Sprintf("Project %s synced successfully", projectKey),
	}, nil
}

func toGRPCError(err error) error {
	msg := fmt.Sprintf("connector error: %v", err)

	return fmt.Errorf("grpc handler: %w", status.Error(codes.Internal, msg))
}
