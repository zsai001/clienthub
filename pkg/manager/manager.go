package manager

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"text/tabwriter"
	"time"

	"github.com/cltx/clienthub/pkg/crypto"
	"github.com/cltx/clienthub/pkg/proto"
	"github.com/cltx/clienthub/pkg/tunnel"
	"go.uber.org/zap"
)

type Manager struct {
	addr   string
	cipher *crypto.Cipher
	logger *zap.Logger
}

func New(addr, secret string, logger *zap.Logger) *Manager {
	salt := []byte("clienthub-fixed-salt-v1")
	cipher := crypto.NewCipherFromPassword(secret, salt)
	return &Manager{
		addr:   addr,
		cipher: cipher,
		logger: logger,
	}
}

func (m *Manager) connect() (net.Conn, *tunnel.ConnWriter, error) {
	conn, err := net.DialTimeout("tcp", m.addr, 5*time.Second)
	if err != nil {
		return nil, nil, fmt.Errorf("connect to admin: %w", err)
	}

	writer := tunnel.NewConnWriter(conn, m.cipher, m.logger)

	token := crypto.ComputeAuthToken("manager", m.cipher.Key())
	payload, _ := proto.EncodeJSON(&proto.AuthPayload{
		ClientName: "manager",
		Token:      token,
	})
	if err := writer.WriteMessage(proto.NewMessage(proto.MsgAuth, 0, payload)); err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("send auth: %w", err)
	}

	resp, err := tunnel.ReadEncryptedMessage(conn, m.cipher)
	if err != nil {
		conn.Close()
		return nil, nil, fmt.Errorf("read auth response: %w", err)
	}
	if resp.Type != proto.MsgAuthOK {
		conn.Close()
		return nil, nil, fmt.Errorf("authentication failed")
	}

	return conn, writer, nil
}

func (m *Manager) sendCommand(msgType proto.MsgType, payload []byte) (*proto.ResponsePayload, error) {
	conn, writer, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	if err := writer.WriteMessage(proto.NewMessage(msgType, 0, payload)); err != nil {
		return nil, fmt.Errorf("send command: %w", err)
	}

	resp, err := tunnel.ReadEncryptedMessage(conn, m.cipher)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.Type != proto.MsgResponse {
		return nil, fmt.Errorf("unexpected response type: %s", resp.Type)
	}

	result, err := proto.DecodeJSON[proto.ResponsePayload](resp.Payload)
	if err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}

	return result, nil
}

func (m *Manager) ListClients() error {
	resp, err := m.sendCommand(proto.MsgListClients, nil)
	if err != nil {
		return err
	}

	fmt.Println(resp.Message)
	if len(resp.Data) == 0 {
		return nil
	}

	var clients []proto.ClientInfo
	if err := json.Unmarshal(resp.Data, &clients); err != nil {
		return fmt.Errorf("decode clients: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "NAME\tADDRESS\tSERVICES\tCONNECTED")
	for _, c := range clients {
		services := ""
		for i, s := range c.Services {
			if i > 0 {
				services += ", "
			}
			services += fmt.Sprintf("%s(%s:%d)", s.Name, s.Protocol, s.Port)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Name, c.Addr, services, c.ConnectedAt)
	}
	w.Flush()
	return nil
}

func (m *Manager) ListTunnels() error {
	resp, err := m.sendCommand(proto.MsgListTunnels, nil)
	if err != nil {
		return err
	}

	fmt.Println(resp.Message)
	if len(resp.Data) == 0 {
		return nil
	}

	var tunnels []proto.TunnelInfo
	if err := json.Unmarshal(resp.Data, &tunnels); err != nil {
		return fmt.Errorf("decode tunnels: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "SESSION\tSOURCE\tTARGET\tSERVICE\tPROTOCOL")
	for _, t := range tunnels {
		fmt.Fprintf(w, "%d\t%s\t%s\t%s\t%s\n",
			t.SessionID, t.SourceClient, t.TargetClient, t.TargetService, t.Protocol)
	}
	w.Flush()
	return nil
}

func (m *Manager) KickClient(name string) error {
	payload, _ := proto.EncodeJSON(&proto.KickPayload{ClientName: name})
	resp, err := m.sendCommand(proto.MsgKickClient, payload)
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}

func (m *Manager) Status() error {
	resp, err := m.sendCommand(proto.MsgListClients, nil)
	if err != nil {
		return fmt.Errorf("server unreachable: %w", err)
	}

	var clients []proto.ClientInfo
	if len(resp.Data) > 0 {
		_ = json.Unmarshal(resp.Data, &clients)
	}

	resp2, err := m.sendCommand(proto.MsgListTunnels, nil)
	if err != nil {
		return err
	}

	var tunnels []proto.TunnelInfo
	if len(resp2.Data) > 0 {
		_ = json.Unmarshal(resp2.Data, &tunnels)
	}

	fmt.Printf("Server: %s (reachable)\n", m.addr)
	fmt.Printf("Clients: %d\n", len(clients))
	fmt.Printf("Active Tunnels: %d\n", len(tunnels))
	return nil
}
