package cli

import (
	"github.com/jake-landersweb/previewctl/src/version"
	"github.com/spf13/cobra"
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the current version and check for updates",
		Run: func(cmd *cobra.Command, args []string) {
			runVersionCheck()
		},
	}
}

func runVersionCheck() {
	KeyValue("Current", version.Get())
	version.PrintVersionWithUpdateCheck()
}
