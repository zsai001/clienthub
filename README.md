# ClientHub - Port Forwarding Service

A Go-based port forwarding service that enables secure client-to-client traffic relay through a central server. All traffic is encrypted with XChaCha20-Poly1305.

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
                └────────────┘◄───│ Manager  │
                                  │  (CLI)   │
                                  └──────────┘
```

## Build

```bash
# Build all binaries
go build -o bin/hub-server ./cmd/server
go build -o bin/hub-client ./cmd/client
go build -o bin/hubctl     ./cmd/manager
```

## Quick Start

### 1. Start the Server

```bash
./bin/hub-server -config examples/server.yaml
```

### 2. Start Client A

Client A exposes a local web service on port 8080 and wants to access Client B's MySQL on port 3306:

```bash
./bin/hub-client -config examples/client-a.yaml
```

### 3. Start Client B

Client B exposes MySQL on port 3306 and wants to access Client A's web service:

```bash
./bin/hub-client -config examples/client-b.yaml
```

### 4. Use the Tunnel

From Client A's machine, connect to MySQL through the local proxy:

```bash
mysql -h 127.0.0.1 -P 13306 -u root
```

From Client B's machine, access Client A's web service:

```bash
curl http://127.0.0.1:18080
```

### 5. Manage with CLI

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

| Field       | Description                  | Default  |
|------------|------------------------------|----------|
| server_addr | Server address (required)    | -        |
| client_name | Unique client name (required)| -        |
| secret      | Pre-shared key (required)    | -        |
| log_level   | Log level                    | `info`   |
| expose      | List of local services       | `[]`     |
| forward     | List of forward rules        | `[]`     |

#### Expose entry

```yaml
expose:
  - name: "web"           # Service name (referenced by other clients)
    local_addr: "127.0.0.1:8080"  # Local address of the service
    protocol: "tcp"        # tcp or udp
```

#### Forward entry

```yaml
forward:
  - remote_client: "client-b"    # Target client name
    remote_service: "mysql"       # Target service name
    listen_addr: "127.0.0.1:13306"  # Local listen address for proxy
    protocol: "tcp"                # tcp or udp
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

## Security

- **Encryption**: XChaCha20-Poly1305 AEAD for all traffic
- **Key Derivation**: Argon2id (memory-hard KDF) from pre-shared password
- **Authentication**: HMAC-SHA256 token verification on connect
- **Integrity**: Poly1305 MAC on every frame prevents tampering
