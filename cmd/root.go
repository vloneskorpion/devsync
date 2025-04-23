/*
Copyright © 2025 Kamil Nowak kamilnowak432@gmail.com
*/
package cmd

import (
	"devsync/sync"
	"fmt"
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

	rootCmd = &cobra.Command{
		Use:   "devsync",
		Short: "Devsync - your local to remote sync tool",
		Long:  `Devsync - your local to remote sync tool`,
		// Uncomment the following line if your bare application
		// has an action associated with it:
		// Run: func(cmd *cobra.Command, args []string) { },
		Run: func(cmd *cobra.Command, args []string) {
			s := sync.NewSyncer(localPath, remotePath, sync.Config{User: user, Ip: ip, Password: password, Port: port})
			err := s.Run()
			if err != nil {
				fmt.Println("Failed to run syncer:", err)
				return
			}
		},
	}
)

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main.main(). It only needs to happen once to the rootCmd.
func Execute() {
	err := rootCmd.Execute()
	if err != nil {
		slog.Error(err.Error())
		os.Exit(1)
	}
}

func init() {
	// Here you will define your flags and configuration settings.
	// Cobra supports persistent flags, which, if defined here,
	// will be global for your application.

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.devsync.yaml)")

	rootCmd.Flags().StringVarP(&localPath, "local", "l", "", "Local path to sync (required)")
	rootCmd.Flags().StringVarP(&remotePath, "remote", "r", "", "Remote path to sync (required)")
	rootCmd.Flags().StringVarP(&user, "user", "u", "", "Remote user (required)")
	rootCmd.Flags().StringVarP(&ip, "ip", "i", "", "Remote IP (required)")
	rootCmd.Flags().StringVarP(&password, "password", "p", "", "Remote password (required)")
	rootCmd.Flags().StringVarP(&port, "port", "o", "22", "Remote port (optional), defaults to 22")

	// add here option to check if user wants to delete not matching remote files or not

	rootCmd.MarkFlagRequired("local")
	rootCmd.MarkFlagRequired("remote")
	rootCmd.MarkFlagRequired("user")
	rootCmd.MarkFlagRequired("ip")
	rootCmd.MarkFlagRequired("password")
}
