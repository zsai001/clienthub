package proto

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const (
	ProtocolVersion byte = 0x01
	HeaderSize           = 1 + 1 + 4 + 4 // version(1) + type(1) + sessionID(4) + length(4)
	MaxPayloadSize       = 1 << 20        // 1 MB
)

type MsgType byte

const (
	MsgAuth        MsgType = 0x01
	MsgAuthOK      MsgType = 0x02
	MsgAuthFail    MsgType = 0x03
	MsgRegister    MsgType = 0x04
	MsgOpenTunnel  MsgType = 0x05
	MsgTunnelReady MsgType = 0x06
	MsgData        MsgType = 0x07
	MsgDataUDP     MsgType = 0x08
	MsgClose       MsgType = 0x09
	MsgHeartbeat   MsgType = 0x0A
	MsgListClients  MsgType = 0x0B
	MsgListTunnels  MsgType = 0x0C
	MsgKickClient   MsgType = 0x0D
	MsgResponse     MsgType = 0x0E
	MsgTunnelFail   MsgType = 0x0F
	MsgAddForward    MsgType = 0x10
	MsgRemoveForward MsgType = 0x11
	MsgListForwards  MsgType = 0x12
	MsgPushConfig    MsgType = 0x13 // server -> client: push stored expose+forward rules
	MsgAddExpose     MsgType = 0x14 // admin -> server: add expose rule for a client
	MsgRemoveExpose  MsgType = 0x15 // admin -> server: remove expose rule
	MsgListExpose    MsgType = 0x16 // admin -> server: list expose rules
	MsgSpeedTest     MsgType = 0x17 // server -> client: speed probe (payload = random data)
	MsgSpeedResult   MsgType = 0x18 // client -> server: speed result
)

func (m MsgType) String() string {
	names := map[MsgType]string{
		MsgAuth:        "AUTH",
		MsgAuthOK:      "AUTH_OK",
		MsgAuthFail:    "AUTH_FAIL",
		MsgRegister:    "REGISTER",
		MsgOpenTunnel:  "OPEN_TUNNEL",
		MsgTunnelReady: "TUNNEL_READY",
		MsgData:        "DATA",
		MsgDataUDP:     "DATA_UDP",
		MsgClose:       "CLOSE",
		MsgHeartbeat:   "HEARTBEAT",
		MsgListClients: "LIST_CLIENTS",
		MsgListTunnels: "LIST_TUNNELS",
		MsgKickClient:    "KICK_CLIENT",
		MsgResponse:      "RESPONSE",
		MsgTunnelFail:    "TUNNEL_FAIL",
		MsgAddForward:    "ADD_FORWARD",
		MsgRemoveForward: "REMOVE_FORWARD",
		MsgListForwards:  "LIST_FORWARDS",
		MsgPushConfig:    "PUSH_CONFIG",
		MsgAddExpose:     "ADD_EXPOSE",
		MsgRemoveExpose:  "REMOVE_EXPOSE",
		MsgListExpose:    "LIST_EXPOSE",
		MsgSpeedTest:     "SPEED_TEST",
		MsgSpeedResult:   "SPEED_RESULT",
	}
	if name, ok := names[m]; ok {
		return name
	}
	return fmt.Sprintf("UNKNOWN(0x%02x)", byte(m))
}

type Message struct {
	Version   byte
	Type      MsgType
	SessionID uint32
	Payload   []byte
}

func NewMessage(msgType MsgType, sessionID uint32, payload []byte) *Message {
	return &Message{
		Version:   ProtocolVersion,
		Type:      msgType,
		SessionID: sessionID,
		Payload:   payload,
	}
}

func (m *Message) Encode() []byte {
	payloadLen := len(m.Payload)
	buf := make([]byte, HeaderSize+payloadLen)
	buf[0] = m.Version
	buf[1] = byte(m.Type)
	binary.BigEndian.PutUint32(buf[2:6], m.SessionID)
	binary.BigEndian.PutUint32(buf[6:10], uint32(payloadLen))
	if payloadLen > 0 {
		copy(buf[HeaderSize:], m.Payload)
	}
	return buf
}

func ReadMessage(r io.Reader) (*Message, error) {
	header := make([]byte, HeaderSize)
	if _, err := io.ReadFull(r, header); err != nil {
		return nil, err
	}

	version := header[0]
	if version != ProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version: 0x%02x", version)
	}

	msgType := MsgType(header[1])
	sessionID := binary.BigEndian.Uint32(header[2:6])
	payloadLen := binary.BigEndian.Uint32(header[6:10])

	if payloadLen > MaxPayloadSize {
		return nil, fmt.Errorf("payload too large: %d bytes", payloadLen)
	}

	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		if _, err := io.ReadFull(r, payload); err != nil {
			return nil, err
		}
	}

	return &Message{
		Version:   version,
		Type:      msgType,
		SessionID: sessionID,
		Payload:   payload,
	}, nil
}

// Payload structures for JSON-encoded control messages

type AuthPayload struct {
	ClientName string `json:"client_name"`
	Token      []byte `json:"token"` // HMAC of client name with shared key
}

type RegisterPayload struct {
	Services []ServiceInfo `json:"services"`
}

type ServiceInfo struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // "tcp" or "udp"
	Port     int    `json:"port"`
}

type OpenTunnelPayload struct {
	TargetClient  string `json:"target_client"`
	TargetService string `json:"target_service"`
	Protocol      string `json:"protocol"`
}

type TunnelReadyPayload struct {
	SessionID     uint32 `json:"session_id"`
	SourceClient  string `json:"source_client"`
	TargetService string `json:"target_service"`
	Protocol      string `json:"protocol"`
}

type TunnelFailPayload struct {
	Reason string `json:"reason"`
}

type ClientInfo struct {
	Name      string        `json:"name"`
	Addr      string        `json:"addr"`
	Services  []ServiceInfo `json:"services"`
	ConnectedAt string      `json:"connected_at"`
}

type TunnelInfo struct {
	SessionID     uint32 `json:"session_id"`
	SourceClient  string `json:"source_client"`
	TargetClient  string `json:"target_client"`
	TargetService string `json:"target_service"`
	Protocol      string `json:"protocol"`
}

type KickPayload struct {
	ClientName string `json:"client_name"`
}

type AddForwardPayload struct {
	ClientName    string `json:"client_name"`
	ListenAddr    string `json:"listen_addr"`
	RemoteClient  string `json:"remote_client"`
	RemoteService string `json:"remote_service"`
	Protocol      string `json:"protocol"`
}

type RemoveForwardPayload struct {
	ClientName string `json:"client_name"`
	ListenAddr string `json:"listen_addr"`
}

type ForwardInfo struct {
	ClientName    string `json:"client_name"`
	ListenAddr    string `json:"listen_addr"`
	RemoteClient  string `json:"remote_client"`
	RemoteService string `json:"remote_service"`
	Protocol      string `json:"protocol"`
}

type ResponsePayload struct {
	Success bool   `json:"success"`
	Message string `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

var (
	ErrInvalidMessage = errors.New("invalid message")
	ErrAuthFailed     = errors.New("authentication failed")
)

func ReadMessageFromBytes(data []byte) (*Message, error) {
	if len(data) < HeaderSize {
		return nil, fmt.Errorf("data too short for header: %d bytes", len(data))
	}
	version := data[0]
	if version != ProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version: 0x%02x", version)
	}
	msgType := MsgType(data[1])
	sessionID := binary.BigEndian.Uint32(data[2:6])
	payloadLen := binary.BigEndian.Uint32(data[6:10])
	if int(payloadLen) != len(data)-HeaderSize {
		return nil, fmt.Errorf("payload length mismatch: header says %d, got %d", payloadLen, len(data)-HeaderSize)
	}
	var payload []byte
	if payloadLen > 0 {
		payload = make([]byte, payloadLen)
		copy(payload, data[HeaderSize:])
	}
	return &Message{
		Version:   version,
		Type:      msgType,
		SessionID: sessionID,
		Payload:   payload,
	}, nil
}

// ExposeRule describes a service a client exposes locally.
type ExposeRule struct {
	Name      string `json:"name"`
	LocalAddr string `json:"local_addr"`
	Protocol  string `json:"protocol"`
}

// PushConfigPayload is sent by the server to a client after auth to apply stored rules.
type PushConfigPayload struct {
	Expose  []ExposeRule  `json:"expose"`
	Forward []ForwardInfo `json:"forward"`
}

// AddExposePayload is sent by hubctl to add an expose rule for a client on the server.
type AddExposePayload struct {
	ClientName string     `json:"client_name"`
	Rule       ExposeRule `json:"rule"`
}

// RemoveExposePayload removes an expose rule by client + service name.
type RemoveExposePayload struct {
	ClientName  string `json:"client_name"`
	ServiceName string `json:"service_name"`
}

// ExposeListEntry is one row in the list-expose output.
type ExposeListEntry struct {
	ClientName string     `json:"client_name"`
	Rule       ExposeRule `json:"rule"`
}

// ListClientsPayload is the optional payload for MsgListClients.
type ListClientsPayload struct {
	SpeedTest bool `json:"speed_test"` // if true, server measures RTT+throughput for each client
}

// SpeedResultPayload is returned by the client after receiving a MsgSpeedTest probe.
type SpeedResultPayload struct {
	RecvBytes      int64 `json:"recv_bytes"`
	RecvDurationMs int64 `json:"recv_duration_ms"` // time from first byte to last byte on client side
}

// ClientSpeedInfo extends ClientInfo with measured RTT and throughput.
type ClientSpeedInfo struct {
	ClientInfo
	RTTMs          int64 `json:"rtt_ms"`          // -1 = not measured
	ThroughputKBps int64 `json:"throughput_kbps"` // -1 = not measured
}

func EncodeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

func DecodeJSON[T any](data []byte) (*T, error) {
	var v T
	if err := json.Unmarshal(data, &v); err != nil {
		return nil, err
	}
	return &v, nil
}
