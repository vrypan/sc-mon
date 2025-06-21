package cmd

import (
	"fmt"
	"os"
	"snapchain-monitor/ui"

	"github.com/spf13/cobra"
)

var Version string
var host string

var rootCmd = &cobra.Command{
	Use:   "sc-mon",
	Short: "Monitor Snapchain Node",
	Run: func(cmd *cobra.Command, args []string) {
		url := fmt.Sprintf("http://%s/v1/info", host)
		ui.Run(url)
	},
}

func Execute() {
	rootCmd.PersistentFlags().StringVar(&host, "host", "localhost:3381", "Snapchain HTTP API host:port")
	if err := rootCmd.Execute(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
