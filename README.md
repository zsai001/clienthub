# ClientHub - Port Forwarding Service

A Go-based port forwarding service that enables secure client-to-client traffic relay through a central server. All traffic is encrypted with XChaCha20-Poly1305. Tunnels are created dynamically via the `hubctl` management CLI.

## Architecture

```
┌──────────┐                          ┌──────────┐
│ Client A │◄──encrypted tunnel──►    │ Client B │
│          │                     │    │          │
│ web:8080 │    ┌────────────┐   │    │ mysql:   │
│          │◄──►│   Server   │◄──┘    │    3306  │
└──────────┘    │            │◄──────►└──────────┘
                │ :7900 TCP  │
                │ :7901 UDP  │
                │ :7902 Admin│    ┌──────────┐
                └────────────┘◄───│  hubctl  │
                                  │  (CLI)   │
                                  └──────────┘
```

## Install

### One-line Install (Linux / macOS / FreeBSD)

```bash
# Stable channel (default)
curl -fsSL https://raw.githubusercontent.com/cltx/clienthub/main/install.sh | bash

# Dev channel (latest development build)
curl -fsSL https://raw.githubusercontent.com/cltx/clienthub/main/install.sh | bash -s -- --channel dev

# Install only a specific component
curl -fsSL https://raw.githubusercontent.com/cltx/clienthub/main/install.sh | bash -s -- --component client
```

### One-line Install (Windows PowerShell)

```powershell
# Stable channel
irm https://raw.githubusercontent.com/cltx/clienthub/main/install.ps1 | iex

# Dev channel
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/cltx/clienthub/main/install.ps1))) -Channel dev
```

### Install Options

| Option | Values | Default | Description |
|--------|--------|---------|-------------|
| `--channel` / `-Channel` | `stable`, `dev` | `stable` | Release channel |
| `--install-dir` / `-InstallDir` | PATH | `~/.local/bin` (Linux/Mac) or `%LOCALAPPDATA%\clienthub\bin` (Windows) | Installation directory |
| `--component` / `-Component` | `all`, `server`, `client`, `hubctl` | `all` | Component to install |

To update, simply run the install command again. It will overwrite the existing binaries.

### Build from Source

```bash
# Build for current platform
make build

# Cross-compile for all platforms
make cross

# Cross-compile + create archives (.tar.gz / .zip)
make package
```

## Quick Start

### 1. Start the Server

```bash
./bin/hub-server -config examples/server.yaml
```

### 2. Start Clients

Client A exposes a local web service on port 8080:

```bash
./bin/hub-client -config examples/client-a.yaml
```

Client B exposes MySQL on port 3306:

```bash
./bin/hub-client -config examples/client-b.yaml
```

### 3. Create Tunnels Dynamically with hubctl

Tell Client A to proxy local `:13306` to Client B's MySQL:

```bash
./bin/hubctl -s "my-super-secret-key" forward \
  --from client-a --listen :13306 --to client-b --service mysql
```

Tell Client B to proxy local `:18080` to Client A's web service:

```bash
./bin/hubctl -s "my-super-secret-key" forward \
  --from client-b --listen :18080 --to client-a --service web
```

### 4. Use the Tunnels

From Client A's machine, connect to MySQL through the local proxy:

```bash
mysql -h 127.0.0.1 -P 13306 -u root
```

From Client B's machine, access Client A's web service:

```bash
curl http://127.0.0.1:18080
```

### 5. Manage Forwards

```bash
# List all active forwards across all clients
./bin/hubctl -s "my-super-secret-key" list-forwards

# Remove a forward
./bin/hubctl -s "my-super-secret-key" unforward --from client-a --listen :13306
```

### 6. Other Management Commands

```bash
# List connected clients
./bin/hubctl -s "my-super-secret-key" list-clients

# List active tunnels
./bin/hubctl -s "my-super-secret-key" list-tunnels

# Check server status
./bin/hubctl -s "my-super-secret-key" status

# Kick a client
./bin/hubctl -s "my-super-secret-key" kick client-a
```

## Configuration

### Server (`server.yaml`)

| Field        | Description                  | Default  |
|-------------|------------------------------|----------|
| listen_addr | TCP control channel address  | `:7900`  |
| udp_addr    | UDP relay address            | `:7901`  |
| admin_addr  | Admin API address            | `:7902`  |
| secret      | Pre-shared key (required)    | -        |
| log_level   | Log level (debug/info/warn)  | `info`   |

### Client (`client.yaml`)

Clients only need the hub address, a name, and the shared secret. Services to expose are declared in config; forward rules are managed dynamically via `hubctl`.

| Field       | Description                  | Default  |
|------------|------------------------------|----------|
| server_addr | Server address (required)    | -        |
| client_name | Unique client name (required)| -        |
| secret      | Pre-shared key (required)    | -        |
| log_level   | Log level                    | `info`   |
| expose      | List of local services       | `[]`     |
| forward     | Static forward rules (optional, prefer hubctl) | `[]` |

#### Expose entry

```yaml
expose:
  - name: "web"
    local_addr: "127.0.0.1:8080"
    protocol: "tcp"
```

## hubctl Commands

| Command | Description |
|---------|-------------|
| `forward` | Create a dynamic port forward on a client |
| `unforward` | Remove a dynamic port forward |
| `list-forwards` | List all active forwards across clients |
| `list-clients` | List connected clients |
| `list-tunnels` | List active tunnels |
| `status` | Show server status |
| `kick` | Disconnect a client |

### forward

```bash
hubctl -s <secret> forward --from <client> --listen <addr> --to <client> --service <name> [--protocol tcp|udp]
```

### unforward

```bash
hubctl -s <secret> unforward --from <client> --listen <addr>
```

## Protocol

Binary frame protocol with encrypted payloads:

```
┌─────────┬──────┬───────────┬────────┬─────────────────────┐
│ Version │ Type │ SessionID │ Length │ Encrypted Payload   │
│ 1 byte  │ 1 B  │  4 bytes  │ 4 bytes│    N bytes          │
└─────────┴──────┴───────────┴────────┴─────────────────────┘
```

Each frame is encrypted with XChaCha20-Poly1305 (24-byte nonce + AEAD ciphertext). The shared key is derived from the pre-shared secret using Argon2id KDF.

## Development

### Branch Model

The project follows a two-branch GitFlow model:

```
  dev (daily development)          main (stable releases)
  │                                │
  ├── feat/xxx ──► merge to dev    │
  ├── fix/yyy  ──► merge to dev    │
  │                                │
  └──── tested & ready ──────────► merge to main ──► tag v1.x.x
```

| Branch | Purpose | Releases |
|--------|---------|----------|
| `dev`  | Daily development, new features, bug fixes | Auto-published as `dev-latest` pre-release on every push |
| `main` | Stable, production-ready code | Stable release created when a `v*` tag is pushed |

### Workflow

1. **Daily development** happens on `dev` (or feature branches merged into `dev`).
2. Every push to `dev` triggers CI and automatically publishes a **dev pre-release** (`dev-latest` tag).
3. When `dev` is stable and ready for release:
   ```bash
   git checkout main
   git merge dev
   git tag v1.0.0
   git push origin main --tags
   ```
4. The `v*` tag triggers CI and creates a **stable release** with all platform binaries.

### Release Channels

Users can install from either channel:

| Channel | Tag | Install Command |
|---------|-----|-----------------|
| **stable** | `v1.x.x` (latest) | `curl -fsSL .../install.sh \| bash` |
| **dev** | `dev-latest` (rolling) | `curl -fsSL .../install.sh \| bash -s -- --channel dev` |

### CI/CD

Automated via GitHub Actions:

- **CI** (`.github/workflows/ci.yml`) — runs `go vet`, build, and tests on every push/PR to `dev` and `main`.
- **Release** (`.github/workflows/release.yml`) — cross-compiles for 8 platform targets and publishes:
  - Push to `dev` → `dev-latest` pre-release (rolling, auto-replaced)
  - Push tag `v*` → stable release with auto-generated changelog

### Supported Platforms

| OS | Architecture |
|----|-------------|
| Linux | amd64, arm64, arm |
| macOS | amd64 (Intel), arm64 (Apple Silicon) |
| Windows | amd64, arm64 |
| FreeBSD | amd64 |

## Security

- **Encryption**: XChaCha20-Poly1305 AEAD for all traffic
- **Key Derivation**: Argon2id (memory-hard KDF) from pre-shared password
- **Authentication**: HMAC-SHA256 token verification on connect
- **Integrity**: Poly1305 MAC on every frame prevents tampering
