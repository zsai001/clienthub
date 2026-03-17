package main

import (
	"os"

	"github.com/cltx/clienthub/pkg/manager"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	adminAddr string
	secret    string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "hubctl",
		Short: "ClientHub management CLI",
		Long:  "Manage the ClientHub port forwarding server: list clients, tunnels, kick clients, and check status.",
	}

	rootCmd.PersistentFlags().StringVarP(&adminAddr, "addr", "a", "127.0.0.1:7902", "admin API address")
	rootCmd.PersistentFlags().StringVarP(&secret, "secret", "s", "", "shared secret (required)")
	rootCmd.MarkPersistentFlagRequired("secret")

	rootCmd.AddCommand(listClientsCmd())
	rootCmd.AddCommand(listTunnelsCmd())
	rootCmd.AddCommand(kickCmd())
	rootCmd.AddCommand(statusCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newManager() *manager.Manager {
	logger, _ := zap.NewDevelopment()
	return manager.New(adminAddr, secret, logger)
}

func listClientsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-clients",
		Short: "List connected clients",
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().ListClients()
		},
	}
}

func listTunnelsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-tunnels",
		Short: "List active tunnels",
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().ListTunnels()
		},
	}
}

func kickCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "kick [client-name]",
		Short: "Disconnect a client",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().KickClient(args[0])
		},
	}
}

func statusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show server status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().Status()
		},
	}
}

