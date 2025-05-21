package cmd

import (
	"devsync/sync"
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// rootCmd represents the base command when called without any subcommands
var (
	localPath  string
	remotePath string
	user       string
	ip         string
	password   string
	port       string
	privateKey string
	knownHosts string

	rootCmd = &cobra.Command{
		Use:   "devsync",
		Short: "Devsync - your local to remote sync tool",
		Long:  `Devsync - your local to remote sync tool`,
		Run: func(cmd *cobra.Command, args []string) {
			s := sync.NewSyncer(localPath, remotePath, sync.Config{User: user, Ip: ip, Password: password, Port: port, PrivateKey: privateKey, KnownHosts: knownHosts})
			err := s.Run()
			if err != nil {
				slog.Error("Failed to run program", "error", err)
				return
			}
		},
	}
)

func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		slog.Error("Failed to run program", "error", err)
		os.Exit(1)
	}
}

func init() {
	rootCmd.Flags().StringVarP(&localPath, "local", "l", "", "Local path to sync (required)")
	rootCmd.Flags().StringVarP(&remotePath, "remote", "r", "", "Remote path to sync (required)")
	rootCmd.Flags().StringVarP(&user, "user", "u", "", "Remote user (required)")
	rootCmd.Flags().StringVarP(&ip, "ip", "i", "", "Remote IP (required)")
	rootCmd.Flags().StringVarP(&password, "password", "p", "", "Remote password (required)")
	rootCmd.Flags().StringVarP(&port, "port", "o", "22", "Remote port (optional), defaults to 22")
	rootCmd.Flags().StringVarP(&privateKey, "private-key", "k", "", "Path to private key (optional), defaults to ~/.ssh/id_rsa")
	rootCmd.Flags().StringVarP(&knownHosts, "known-hosts", "s", "", "Path to known hosts file (optional), defaults to ~/.ssh/known_hosts")

	rootCmd.MarkFlagRequired("local")
	rootCmd.MarkFlagRequired("remote")
	rootCmd.MarkFlagRequired("user")
	rootCmd.MarkFlagRequired("ip")
	rootCmd.MarkFlagRequired("password")
}
