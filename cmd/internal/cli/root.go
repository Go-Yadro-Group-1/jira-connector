package cli

import (
	_ "embed"

	"github.com/spf13/cobra"
)

const (
	rootShort   = "Jira Connector"
	serviceName = "connector"
)

//go:embed data/root-long-data.txt
var embeddedRootLongData string

func NewRootCmd() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   serviceName,
		Short: rootShort,
		Long:  embeddedRootLongData,
		Args:  cobra.NoArgs,
	}

	return rootCmd
}
