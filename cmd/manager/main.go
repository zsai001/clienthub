package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/cltx/clienthub/config"
	"github.com/cltx/clienthub/pkg/manager"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

var (
	configFile string
	adminAddr  string
	secret     string
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "hubctl",
		Short: "ClientHub management CLI",
		Long:  "Manage the ClientHub port forwarding server: list clients, tunnels, kick clients, and check status.",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			loadDefaults()
		},
	}

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "config file (default: ~/.config/clienthub/hubctl.yaml)")
	rootCmd.PersistentFlags().StringVarP(&adminAddr, "addr", "a", "", "admin API address")
	rootCmd.PersistentFlags().StringVarP(&secret, "secret", "s", "", "shared secret")

	rootCmd.AddCommand(listClientsCmd())
	rootCmd.AddCommand(listTunnelsCmd())
	rootCmd.AddCommand(kickCmd())
	rootCmd.AddCommand(statusCmd())
	rootCmd.AddCommand(forwardCmd())
	rootCmd.AddCommand(unforwardCmd())
	rootCmd.AddCommand(listForwardsCmd())
	rootCmd.AddCommand(exposeCmd())
	rootCmd.AddCommand(unexposeCmd())
	rootCmd.AddCommand(listExposeCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func loadDefaults() {
	if configFile == "" {
		home, _ := os.UserHomeDir()
		configFile = filepath.Join(home, ".config", "clienthub", "hubctl.yaml")
	}
	cfg, err := config.LoadManagerConfig(configFile)
	if err == nil {
		if adminAddr == "" {
			adminAddr = cfg.AdminAddr
		}
		if secret == "" {
			secret = cfg.Secret
		}
	}
	if adminAddr == "" {
		adminAddr = "127.0.0.1:7902"
	}
	if secret == "" {
		fmt.Fprintln(os.Stderr, "Error: secret is required (use -s flag or config file)")
		os.Exit(1)
	}
}

func newManager() *manager.Manager {
	logger, _ := zap.NewDevelopment()
	return manager.New(adminAddr, secret, logger)
}

func listClientsCmd() *cobra.Command {
	var noSpeed bool
	cmd := &cobra.Command{
		Use:   "list-clients",
		Short: "List connected clients",
		Long:  "List connected clients. By default measures RTT and throughput for each client.",
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().ListClients(!noSpeed)
		},
	}
	cmd.Flags().BoolVar(&noSpeed, "no-speed", false, "skip speed test (faster output)")
	return cmd
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

func forwardCmd() *cobra.Command {
	var fromClient, listenAddr, toClient, toService, protocol string
	cmd := &cobra.Command{
		Use:   "forward",
		Short: "Create a dynamic port forward on a client",
		Long:  "Instruct a client to start a local proxy that forwards traffic to another client's service.",
		Example: `  hubctl -s mysecret forward --from client-a --listen :13306 --to client-b --service mysql
  hubctl -s mysecret forward --from client-a --listen 127.0.0.1:18080 --to client-b --service web --protocol tcp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().AddForward(fromClient, listenAddr, toClient, toService, protocol)
		},
	}
	cmd.Flags().StringVar(&fromClient, "from", "", "source client name (required)")
	cmd.Flags().StringVar(&listenAddr, "listen", "", "local listen address on source client (required)")
	cmd.Flags().StringVar(&toClient, "to", "", "target client name (required)")
	cmd.Flags().StringVar(&toService, "service", "", "target service name (required)")
	cmd.Flags().StringVar(&protocol, "protocol", "tcp", "protocol (tcp or udp)")
	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("listen")
	cmd.MarkFlagRequired("to")
	cmd.MarkFlagRequired("service")
	return cmd
}

func unforwardCmd() *cobra.Command {
	var fromClient, listenAddr string
	cmd := &cobra.Command{
		Use:   "unforward",
		Short: "Remove a dynamic port forward from a client",
		Example: `  hubctl -s mysecret unforward --from client-a --listen :13306`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().RemoveForward(fromClient, listenAddr)
		},
	}
	cmd.Flags().StringVar(&fromClient, "from", "", "client name (required)")
	cmd.Flags().StringVar(&listenAddr, "listen", "", "listen address to remove (required)")
	cmd.MarkFlagRequired("from")
	cmd.MarkFlagRequired("listen")
	return cmd
}

func listForwardsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list-forwards",
		Short: "List all active port forwards across clients",
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().ListForwards()
		},
	}
}

func exposeCmd() *cobra.Command {
	var clientName, name, localAddr, protocol string
	cmd := &cobra.Command{
		Use:   "expose",
		Short: "Add an expose rule for a client (stored on server)",
		Example: `  hubctl -s mysecret expose --client hai --name ssh --local 127.0.0.1:22
  hubctl -s mysecret expose --client hai --name web --local 127.0.0.1:8080 --protocol tcp`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().AddExpose(clientName, name, localAddr, protocol)
		},
	}
	cmd.Flags().StringVar(&clientName, "client", "", "client name (required)")
	cmd.Flags().StringVar(&name, "name", "", "service name (required)")
	cmd.Flags().StringVar(&localAddr, "local", "", "local address to expose, e.g. 127.0.0.1:22 (required)")
	cmd.Flags().StringVar(&protocol, "protocol", "tcp", "protocol (tcp or udp)")
	cmd.MarkFlagRequired("client")
	cmd.MarkFlagRequired("name")
	cmd.MarkFlagRequired("local")
	return cmd
}

func unexposeCmd() *cobra.Command {
	var clientName, serviceName string
	cmd := &cobra.Command{
		Use:   "unexpose",
		Short: "Remove an expose rule from a client",
		Example: `  hubctl -s mysecret unexpose --client hai --name ssh`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().RemoveExpose(clientName, serviceName)
		},
	}
	cmd.Flags().StringVar(&clientName, "client", "", "client name (required)")
	cmd.Flags().StringVar(&serviceName, "name", "", "service name to remove (required)")
	cmd.MarkFlagRequired("client")
	cmd.MarkFlagRequired("name")
	return cmd
}

func listExposeCmd() *cobra.Command {
	var clientName string
	cmd := &cobra.Command{
		Use:   "list-expose",
		Short: "List stored expose rules on the server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return newManager().ListExpose(clientName)
		},
	}
	cmd.Flags().StringVar(&clientName, "client", "", "filter by client name (optional)")
	return cmd
}

