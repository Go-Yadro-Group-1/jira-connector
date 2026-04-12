package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	connectorv1 "github.com/Go-Yadro-Group-1/Jira-Connector/gen/proto/connector/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

const (
	defaultLimit = 10
)

func main() {
	code := run()
	os.Exit(code)
}

func run() int { //nolint:cyclop
	addr := flag.String("addr", "localhost:50052", "gRPC server address")
	cmd := flag.String("cmd", "projects", "command: projects or download")
	key := flag.String("key", "", "project key (for download)")
	force := flag.Bool("force", false, "force re-download (for download)")
	search := flag.String("search", "", "search query (for projects)")
	limit := flag.Int("limit", defaultLimit, "limit per page (for projects)")
	page := flag.Int("page", 0, "page number (for projects)")
	flag.Parse()

	conn, err := grpc.NewClient(*addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Printf("failed to connect: %v", err)

		return 1
	}
	defer conn.Close()

	client := connectorv1.NewConnectorServiceClient(conn)
	ctx := context.Background()

	switch *cmd {
	case "projects":
		return listProjects(ctx, client, *search, *limit, *page)
	case "download":
		if *key == "" {
			log.Print("flag -key is required for download command")

			return 1
		}

		return downloadProject(ctx, client, *key, *force)
	default:
		log.Printf("unknown command: %s (use 'projects' or 'download')", *cmd)

		return 1
	}
}

func listProjects(
	ctx context.Context,
	client connectorv1.ConnectorServiceClient,
	search string,
	limit,
	page int,
) int {
	resp, err := client.GetAvailableProjects(ctx, &connectorv1.GetAvailableProjectsRequest{
		SearchQuery: search,
		Limit:       int32(limit), //nolint:gosec
		Page:        int32(page),  //nolint:gosec
	})
	if err != nil {
		log.Printf("GetAvailableProjects error: %v", err)

		return 1
	}

	projects := resp.GetProjects()

	fmt.Fprintf(os.Stdout, "Found %d projects (total: %d, is_last: %v):\n",
		len(projects), resp.GetTotal(), resp.GetIsLast())

	for _, p := range projects {
		fmt.Fprintf(os.Stdout, "  [%s] %s\n    %s\n", p.GetKey(), p.GetTitle(), p.GetSelf())
	}

	return 0
}

func downloadProject(
	ctx context.Context,
	client connectorv1.ConnectorServiceClient,
	key string,
	force bool,
) int {
	resp, err := client.DownloadProject(ctx, &connectorv1.DownloadProjectRequest{
		ProjectKey: key,
		Force:      force,
	})
	if err != nil {
		log.Printf("DownloadProject error: %v", err)

		return 1
	}

	fmt.Fprintf(os.Stdout, "Sync ID:   %s\nStatus:    %s\nMessage:   %s\n",
		resp.GetSyncId(), resp.GetStatus(), resp.GetMessage())

	return 0
}
