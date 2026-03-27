package main

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"github.com/cltx/clienthub/config"
	"github.com/cltx/clienthub/pkg/manager"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
	"gopkg.in/yaml.v3"
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
	}

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "", "config file (default: ~/.config/clienthub/hubctl.yaml)")
	rootCmd.PersistentFlags().StringVarP(&adminAddr, "addr", "a", "", "admin API address")
	rootCmd.PersistentFlags().StringVarP(&secret, "secret", "s", "", "shared secret")

	// These commands don't need server connection
	localCmds := map[string]bool{"install": true, "gen-token": true, "config": true}
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if localCmds[cmd.Name()] {
			return
		}
		loadDefaults()
	}

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
	rootCmd.AddCommand(installCmd())
	rootCmd.AddCommand(genTokenCmd())
	rootCmd.AddCommand(configCmd())

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

// ── install command ─────────────────────────────────────────────────

const systemdServerTpl = `[Unit]
Description=ClientHub Server
After=network.target

[Service]
Type=simple
ExecStart={{.BinPath}} -config {{.ConfigPath}}
Restart=always
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
`

const systemdClientTpl = `[Unit]
Description=ClientHub Client
After=network.target

[Service]
Type=simple
ExecStart={{.BinPath}} -config {{.ConfigPath}}
Restart=always
RestartSec=5

[Install]
WantedBy=multi-user.target
`

const launchdServerTpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.clienthub.server</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinPath}}</string>
        <string>-config</string>
        <string>{{.ConfigPath}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/hub-server.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/hub-server.err.log</string>
</dict>
</plist>
`

const launchdClientTpl = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.clienthub.client</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinPath}}</string>
        <string>-config</string>
        <string>{{.ConfigPath}}</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogDir}}/hub-client.log</string>
    <key>StandardErrorPath</key>
    <string>{{.LogDir}}/hub-client.err.log</string>
</dict>
</plist>
`

type serviceVars struct {
	BinPath    string
	ConfigPath string
	LogDir     string
}

func installCmd() *cobra.Command {
	var cfgPath string

	cmd := &cobra.Command{
		Use:   "install [server|client]",
		Short: "Install hub-server or hub-client as a system daemon",
		Long: `Install a ClientHub component as a system service.

On Linux:  creates a systemd unit and enables it.
On macOS:  creates a launchd plist and loads it.`,
		Example: `  # Install server as daemon
  hubctl install server -c /etc/clienthub/server.yaml

  # Install client as daemon (uses default config path)
  hubctl install client

  # Install client with custom config
  hubctl install client -c ~/.config/clienthub/client.yaml`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			component := args[0]
			if component != "server" && component != "client" {
				return fmt.Errorf("component must be 'server' or 'client', got '%s'", component)
			}
			return installDaemon(component, cfgPath)
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "path to config file")
	return cmd
}

func installDaemon(component, cfgPath string) error {
	// Resolve binary path
	var binName string
	if component == "server" {
		binName = "hub-server"
	} else {
		binName = "hub-client"
	}

	binPath, err := exec.LookPath(binName)
	if err != nil {
		// Try common install locations
		home, _ := os.UserHomeDir()
		for _, dir := range []string{"/usr/local/bin", home + "/.local/bin", "/opt/clienthub"} {
			p := filepath.Join(dir, binName)
			if _, err := os.Stat(p); err == nil {
				binPath = p
				break
			}
		}
		if binPath == "" {
			return fmt.Errorf("%s not found in PATH or common locations; install it first", binName)
		}
	}
	binPath, _ = filepath.Abs(binPath)

	// Resolve config path
	if cfgPath == "" {
		home, _ := os.UserHomeDir()
		if component == "server" {
			cfgPath = filepath.Join(home, ".config", "clienthub", "server.yaml")
		} else {
			cfgPath = filepath.Join(home, ".config", "clienthub", "client.yaml")
		}
	}
	cfgPath, _ = filepath.Abs(cfgPath)

	if _, err := os.Stat(cfgPath); os.IsNotExist(err) {
		return fmt.Errorf("config file not found: %s\nCreate it first or specify with -c", cfgPath)
	}

	switch runtime.GOOS {
	case "linux":
		return installSystemd(component, binPath, cfgPath)
	case "darwin":
		return installLaunchd(component, binPath, cfgPath)
	default:
		return fmt.Errorf("daemon install not supported on %s; run %s manually or create a service yourself", runtime.GOOS, binName)
	}
}

func installSystemd(component, binPath, cfgPath string) error {
	unitName := "clienthub-" + component + ".service"
	unitPath := "/etc/systemd/system/" + unitName

	var tplStr string
	if component == "server" {
		tplStr = systemdServerTpl
	} else {
		tplStr = systemdClientTpl
	}

	tpl, _ := template.New("unit").Parse(tplStr)
	var buf strings.Builder
	tpl.Execute(&buf, serviceVars{BinPath: binPath, ConfigPath: cfgPath})

	if err := os.WriteFile(unitPath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write %s: %w (try running with sudo)", unitPath, err)
	}

	fmt.Printf("Created %s\n", unitPath)

	cmds := [][]string{
		{"systemctl", "daemon-reload"},
		{"systemctl", "enable", unitName},
		{"systemctl", "restart", unitName},
	}
	for _, c := range cmds {
		out, err := exec.Command(c[0], c[1:]...).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%s failed: %s\n%s", strings.Join(c, " "), err, string(out))
		}
	}

	fmt.Printf("Service %s installed and started.\n", unitName)
	fmt.Printf("  Status:  systemctl status %s\n", unitName)
	fmt.Printf("  Logs:    journalctl -u %s -f\n", unitName)
	fmt.Printf("  Stop:    systemctl stop %s\n", unitName)
	fmt.Printf("  Remove:  systemctl disable %s && rm %s\n", unitName, unitPath)
	return nil
}

func installLaunchd(component, binPath, cfgPath string) error {
	home, _ := os.UserHomeDir()
	logDir := filepath.Join(home, "Library", "Logs", "ClientHub")
	os.MkdirAll(logDir, 0755)

	label := "com.clienthub." + component
	plistName := label + ".plist"
	plistPath := filepath.Join(home, "Library", "LaunchAgents", plistName)
	os.MkdirAll(filepath.Dir(plistPath), 0755)

	var tplStr string
	if component == "server" {
		tplStr = launchdServerTpl
	} else {
		tplStr = launchdClientTpl
	}

	tpl, _ := template.New("plist").Parse(tplStr)
	var buf strings.Builder
	tpl.Execute(&buf, serviceVars{BinPath: binPath, ConfigPath: cfgPath, LogDir: logDir})

	// Unload existing service if present
	exec.Command("launchctl", "bootout", fmt.Sprintf("gui/%d/%s", os.Getuid(), label)).Run()

	if err := os.WriteFile(plistPath, []byte(buf.String()), 0644); err != nil {
		return fmt.Errorf("write %s: %w", plistPath, err)
	}

	fmt.Printf("Created %s\n", plistPath)

	out, err := exec.Command("launchctl", "bootstrap", fmt.Sprintf("gui/%d", os.Getuid()), plistPath).CombinedOutput()
	if err != nil {
		// Fallback to legacy load
		out, err = exec.Command("launchctl", "load", "-w", plistPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("launchctl load failed: %s\n%s", err, string(out))
		}
	}

	fmt.Printf("Service %s installed and started.\n", label)
	fmt.Printf("  Logs:    tail -f %s/hub-%s.log\n", logDir, component)
	fmt.Printf("  Stop:    launchctl bootout gui/%d/%s\n", os.Getuid(), label)
	fmt.Printf("  Remove:  launchctl bootout gui/%d/%s && rm %s\n", os.Getuid(), label, plistPath)
	return nil
}

// ── gen-token command ───────────────────────────────────────────────

func genTokenCmd() *cobra.Command {
	var length int
	cmd := &cobra.Command{
		Use:   "gen-token",
		Short: "Generate a random secret token for server/client auth",
		Long: `Generate a cryptographically secure random token.
Use this as the "secret" field in server and client configs.`,
		Example: `  # Generate a token (default 32 bytes / 64 hex chars)
  hubctl gen-token

  # Generate a shorter token
  hubctl gen-token --length 16`,
		RunE: func(cmd *cobra.Command, args []string) error {
			buf := make([]byte, length)
			if _, err := rand.Read(buf); err != nil {
				return fmt.Errorf("generate token: %w", err)
			}
			token := hex.EncodeToString(buf)
			fmt.Println(token)
			return nil
		},
	}
	cmd.Flags().IntVar(&length, "length", 32, "token length in bytes (output is hex, so 2x characters)")
	return cmd
}

// ── config command ──────────────────────────────────────────────────

func configCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "View or modify hubctl / client configuration",
		Long: `View, set, or edit configuration files.

When called without subcommands, saves the hubctl manager config
(admin address and secret) from -a and -s flags to hubctl.yaml.
Use subcommands (show, set, init) to manage client config.`,
		Example: `  # Save hubctl manager config
  hubctl config -a 8.146.210.7:7902 -s "my-secret"

  # Show current hubctl manager config
  hubctl config`,
		RunE: func(cmd *cobra.Command, args []string) error {
			cfgPath := configFile
			if cfgPath == "" {
				home, _ := os.UserHomeDir()
				cfgPath = filepath.Join(home, ".config", "clienthub", "hubctl.yaml")
			}

			// If flags provided, save them
			if adminAddr != "" || secret != "" {
				// Load existing config if present
				raw := make(map[string]interface{})
				if data, err := os.ReadFile(cfgPath); err == nil {
					yaml.Unmarshal(data, &raw)
				}
				if adminAddr != "" {
					raw["admin_addr"] = adminAddr
				}
				if secret != "" {
					raw["secret"] = secret
				}
				out, _ := yaml.Marshal(raw)
				os.MkdirAll(filepath.Dir(cfgPath), 0755)
				if err := os.WriteFile(cfgPath, out, 0644); err != nil {
					return fmt.Errorf("write config: %w", err)
				}
				fmt.Printf("Config saved to %s\n", cfgPath)
				return nil
			}

			// No flags: show current config
			data, err := os.ReadFile(cfgPath)
			if err != nil {
				return fmt.Errorf("no hubctl config found at %s\nUse: hubctl config -a <addr> -s <secret>", cfgPath)
			}
			fmt.Printf("# %s\n", cfgPath)
			fmt.Print(string(data))
			return nil
		},
	}

	cmd.AddCommand(configShowCmd())
	cmd.AddCommand(configSetCmd())
	cmd.AddCommand(configInitCmd())
	return cmd
}

func resolveClientConfig(flagPath string) string {
	if flagPath != "" {
		return flagPath
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "clienthub", "client.yaml")
}

func configShowCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "show",
		Short: "Show current client config",
		RunE: func(cmd *cobra.Command, args []string) error {
			p := resolveClientConfig(cfgPath)
			data, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read config: %w", err)
			}
			fmt.Printf("# %s\n", p)
			fmt.Print(string(data))
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	return cmd
}

func configSetCmd() *cobra.Command {
	var cfgPath string
	cmd := &cobra.Command{
		Use:   "set <key> <value>",
		Short: "Set a config field (e.g. server_addr, client_name, secret)",
		Long: `Set a top-level field in the client YAML config.

Supported keys: server_addr, client_name, secret, log_level`,
		Example: `  # Change hub server endpoint
  hubctl config set server_addr 192.168.1.100:7900

  # Change client name
  hubctl config set client_name my-laptop

  # Change secret
  hubctl config set secret my-new-secret

  # Change log level
  hubctl config set log_level debug`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			key, value := args[0], args[1]

			allowed := map[string]bool{
				"server_addr": true,
				"client_name": true,
				"secret":      true,
				"log_level":   true,
			}
			if !allowed[key] {
				return fmt.Errorf("unknown key '%s'; supported: server_addr, client_name, secret, log_level", key)
			}

			p := resolveClientConfig(cfgPath)

			// Read existing config as raw map to preserve structure
			raw := make(map[string]interface{})
			data, err := os.ReadFile(p)
			if err != nil {
				return fmt.Errorf("read config: %w", err)
			}
			if err := yaml.Unmarshal(data, &raw); err != nil {
				return fmt.Errorf("parse config: %w", err)
			}

			old, _ := raw[key].(string)
			raw[key] = value

			out, err := yaml.Marshal(raw)
			if err != nil {
				return fmt.Errorf("marshal config: %w", err)
			}
			if err := os.WriteFile(p, out, 0644); err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			if old != "" {
				fmt.Printf("%s: %s -> %s\n", key, old, value)
			} else {
				fmt.Printf("%s: %s\n", key, value)
			}
			fmt.Printf("Config saved to %s\n", p)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	return cmd
}

func configInitCmd() *cobra.Command {
	var cfgPath, serverAddr, clientName, secretVal string
	cmd := &cobra.Command{
		Use:   "init",
		Short: "Create a new client config file",
		Example: `  hubctl config init --server 192.168.1.100:7900 --name my-laptop --secret mytoken
  hubctl config init -c /etc/clienthub/client.yaml --server hub.example.com:7900`,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := resolveClientConfig(cfgPath)

			if _, err := os.Stat(p); err == nil {
				return fmt.Errorf("config already exists: %s (use 'hubctl config set' to modify)", p)
			}

			if serverAddr == "" {
				return fmt.Errorf("--server is required")
			}
			if clientName == "" {
				h, _ := os.Hostname()
				clientName = h
			}
			if secretVal == "" {
				buf := make([]byte, 32)
				rand.Read(buf)
				secretVal = hex.EncodeToString(buf)
				fmt.Printf("Generated secret: %s\n", secretVal)
			}

			cfg := map[string]interface{}{
				"server_addr": serverAddr,
				"client_name": clientName,
				"secret":      secretVal,
				"log_level":   "info",
				"expose":      []interface{}{},
			}

			out, _ := yaml.Marshal(cfg)
			os.MkdirAll(filepath.Dir(p), 0755)
			if err := os.WriteFile(p, out, 0644); err != nil {
				return fmt.Errorf("write config: %w", err)
			}

			fmt.Printf("Config created: %s\n", p)
			return nil
		},
	}
	cmd.Flags().StringVarP(&cfgPath, "config", "c", "", "config file path")
	cmd.Flags().StringVar(&serverAddr, "server", "", "hub server address (required)")
	cmd.Flags().StringVar(&clientName, "name", "", "client name (default: hostname)")
	cmd.Flags().StringVar(&secretVal, "secret", "", "shared secret (default: auto-generate)")
	return cmd
}

