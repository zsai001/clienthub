package manager

import (
	"bufio"
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

type adminConn struct {
	conn   net.Conn
	writer *tunnel.ConnWriter
	br     *bufio.Reader
}

func (m *Manager) connect() (*adminConn, error) {
	conn, err := net.DialTimeout("tcp", m.addr, 5*time.Second)
	if err != nil {
		return nil, fmt.Errorf("connect to admin: %w", err)
	}

	writer := tunnel.NewConnWriter(conn, m.cipher, m.logger)
	br := tunnel.NewBufReader(conn)

	token := crypto.ComputeAuthToken("manager", m.cipher.Key())
	payload, _ := proto.EncodeJSON(&proto.AuthPayload{
		ClientName: "manager",
		Token:      token,
	})
	if err := writer.WriteMessage(proto.NewMessage(proto.MsgAuth, 0, payload)); err != nil {
		conn.Close()
		return nil, fmt.Errorf("send auth: %w", err)
	}

	resp, err := tunnel.ReadEncryptedMessage(br, m.cipher)
	if err != nil {
		conn.Close()
		return nil, fmt.Errorf("read auth response: %w", err)
	}
	if resp.Type != proto.MsgAuthOK {
		conn.Close()
		return nil, fmt.Errorf("authentication failed")
	}

	return &adminConn{conn: conn, writer: writer, br: br}, nil
}

// Response is an alias for proto.ResponsePayload, exported for use by the C bridge.
type Response = proto.ResponsePayload

// SendCommand sends an admin command and returns the parsed response.
func (m *Manager) SendCommand(msgType proto.MsgType, payload []byte) (*proto.ResponsePayload, error) {
	ac, err := m.connect()
	if err != nil {
		return nil, err
	}
	defer ac.conn.Close()

	if err := ac.writer.WriteMessage(proto.NewMessage(msgType, 0, payload)); err != nil {
		return nil, fmt.Errorf("send command: %w", err)
	}

	resp, err := tunnel.ReadEncryptedMessage(ac.br, m.cipher)
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

func (m *Manager) ListClients(speedTest bool) error {
	var payload []byte
	if speedTest {
		fmt.Println("Running speed test, please wait...")
		payload, _ = proto.EncodeJSON(&proto.ListClientsPayload{SpeedTest: true})
	}
	resp, err := m.SendCommand(proto.MsgListClients, payload)
	if err != nil {
		return err
	}

	fmt.Println(resp.Message)
	if len(resp.Data) == 0 {
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)

	if speedTest {
		var clients []proto.ClientSpeedInfo
		if err := json.Unmarshal(resp.Data, &clients); err != nil {
			return fmt.Errorf("decode clients: %w", err)
		}
		fmt.Fprintln(w, "NAME\tADDRESS\tSERVICES\tRTT\tSPEED\tCONNECTED")
		for _, c := range clients {
			services := formatServices(c.Services)
			rtt := "-"
			if c.RTTMs >= 0 {
				rtt = fmt.Sprintf("%dms", c.RTTMs)
			}
			speed := "-"
			if c.ThroughputKBps >= 0 {
				if c.ThroughputKBps >= 1024 {
					speed = fmt.Sprintf("%.1fMB/s", float64(c.ThroughputKBps)/1024)
				} else {
					speed = fmt.Sprintf("%dKB/s", c.ThroughputKBps)
				}
			}
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", c.Name, c.Addr, services, rtt, speed, c.ConnectedAt)
		}
	} else {
		var clients []proto.ClientInfo
		if err := json.Unmarshal(resp.Data, &clients); err != nil {
			return fmt.Errorf("decode clients: %w", err)
		}
		fmt.Fprintln(w, "NAME\tADDRESS\tSERVICES\tCONNECTED")
		for _, c := range clients {
			fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", c.Name, c.Addr, formatServices(c.Services), c.ConnectedAt)
		}
	}
	w.Flush()
	return nil
}

func formatServices(services []proto.ServiceInfo) string {
	s := ""
	for i, svc := range services {
		if i > 0 {
			s += ", "
		}
		s += fmt.Sprintf("%s(%s:%d)", svc.Name, svc.Protocol, svc.Port)
	}
	return s
}

func (m *Manager) ListTunnels() error {
	resp, err := m.SendCommand(proto.MsgListTunnels, nil)
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
	resp, err := m.SendCommand(proto.MsgKickClient, payload)
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	return nil
}

func (m *Manager) Status() error {
	resp, err := m.SendCommand(proto.MsgListClients, nil) // no speed test for status
	if err != nil {
		return fmt.Errorf("server unreachable: %w", err)
	}

	var clients []proto.ClientInfo
	if len(resp.Data) > 0 {
		_ = json.Unmarshal(resp.Data, &clients)
	}

	resp2, err := m.SendCommand(proto.MsgListTunnels, nil)
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

func (m *Manager) AddForward(fromClient, listenAddr, toClient, toService, protocol string) error {
	if protocol == "" {
		protocol = "tcp"
	}
	payload, _ := proto.EncodeJSON(&proto.AddForwardPayload{
		ClientName:    fromClient,
		ListenAddr:    listenAddr,
		RemoteClient:  toClient,
		RemoteService: toService,
		Protocol:      protocol,
	})
	resp, err := m.SendCommand(proto.MsgAddForward, payload)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Message)
	}
	fmt.Println(resp.Message)
	return nil
}

func (m *Manager) RemoveForward(fromClient, listenAddr string) error {
	payload, _ := proto.EncodeJSON(&proto.RemoveForwardPayload{
		ClientName: fromClient,
		ListenAddr: listenAddr,
	})
	resp, err := m.SendCommand(proto.MsgRemoveForward, payload)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Message)
	}
	fmt.Println(resp.Message)
	return nil
}

func (m *Manager) AddExpose(clientName, name, localAddr, protocol string) error {
	if protocol == "" {
		protocol = "tcp"
	}
	payload, _ := proto.EncodeJSON(&proto.AddExposePayload{
		ClientName: clientName,
		Rule: proto.ExposeRule{
			Name:      name,
			LocalAddr: localAddr,
			Protocol:  protocol,
		},
	})
	resp, err := m.SendCommand(proto.MsgAddExpose, payload)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Message)
	}
	fmt.Println(resp.Message)
	return nil
}

func (m *Manager) RemoveExpose(clientName, serviceName string) error {
	payload, _ := proto.EncodeJSON(&proto.RemoveExposePayload{
		ClientName:  clientName,
		ServiceName: serviceName,
	})
	resp, err := m.SendCommand(proto.MsgRemoveExpose, payload)
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("%s", resp.Message)
	}
	fmt.Println(resp.Message)
	return nil
}

func (m *Manager) ListExpose(clientName string) error {
	var payload []byte
	if clientName != "" {
		payload, _ = proto.EncodeJSON(&proto.KickPayload{ClientName: clientName})
	}
	resp, err := m.SendCommand(proto.MsgListExpose, payload)
	if err != nil {
		return err
	}
	fmt.Println(resp.Message)
	if len(resp.Data) == 0 {
		return nil
	}
	var entries []proto.ExposeListEntry
	if err := json.Unmarshal(resp.Data, &entries); err != nil {
		return fmt.Errorf("decode expose list: %w", err)
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CLIENT\tNAME\tLOCAL_ADDR\tPROTOCOL")
	for _, e := range entries {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", e.ClientName, e.Rule.Name, e.Rule.LocalAddr, e.Rule.Protocol)
	}
	w.Flush()
	return nil
}

func (m *Manager) ListForwards() error {
	resp, err := m.SendCommand(proto.MsgListForwards, nil)
	if err != nil {
		return err
	}

	fmt.Println(resp.Message)
	if len(resp.Data) == 0 {
		return nil
	}

	var forwards []proto.ForwardInfo
	if err := json.Unmarshal(resp.Data, &forwards); err != nil {
		return fmt.Errorf("decode forwards: %w", err)
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CLIENT\tLISTEN\tTARGET\tSERVICE\tPROTOCOL")
	for _, f := range forwards {
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
			f.ClientName, f.ListenAddr, f.RemoteClient, f.RemoteService, f.Protocol)
	}
	w.Flush()
	return nil
}
