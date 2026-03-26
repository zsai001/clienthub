package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cltx/clienthub/config"
	"github.com/cltx/clienthub/pkg/crypto"
	"github.com/cltx/clienthub/pkg/proto"
	"github.com/cltx/clienthub/pkg/tunnel"
	"go.uber.org/zap"
)

type Server struct {
	cfg      *config.ServerConfig
	cipher   *crypto.Cipher
	registry *Registry
	relay    *Relay
	store    *Store
	logger   *zap.Logger

	pendingMu   sync.Mutex
	pendingReqs map[uint32]chan *proto.Message // requestID -> response channel
	nextReqID   uint32
}

func New(cfg *config.ServerConfig, logger *zap.Logger) (*Server, error) {
	salt := []byte("clienthub-fixed-salt-v1")
	cipher := crypto.NewCipherFromPassword(cfg.Secret, salt)
	reg := NewRegistry()
	store, err := NewStore(cfg.StorePath)
	if err != nil {
		return nil, fmt.Errorf("load store: %w", err)
	}
	return &Server{
		cfg:         cfg,
		cipher:      cipher,
		registry:    reg,
		relay:       NewRelay(reg, logger),
		store:       store,
		logger:      logger,
		pendingReqs: make(map[uint32]chan *proto.Message),
	}, nil
}

func (s *Server) Run(ctx context.Context) error {
	errCh := make(chan error, 3)

	go func() { errCh <- s.serveTCP(ctx) }()
	go func() { errCh <- s.serveUDP(ctx) }()
	go func() { errCh <- s.serveAdmin(ctx) }()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *Server) serveTCP(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.ListenAddr)
	if err != nil {
		return fmt.Errorf("tcp listen: %w", err)
	}
	defer ln.Close()
	s.logger.Info("TCP control channel listening", zap.String("addr", s.cfg.ListenAddr))

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				s.logger.Error("accept failed", zap.Error(err))
				continue
			}
		}
		go s.handleClient(ctx, conn)
	}
}

func (s *Server) handleClient(ctx context.Context, conn net.Conn) {
	writer := tunnel.NewConnWriter(conn, s.cipher, s.logger)
	br := tunnel.NewBufReader(conn)

	// Read auth message with timeout
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	msg, err := tunnel.ReadEncryptedMessage(br, s.cipher)
	if err != nil {
		s.logger.Warn("auth read failed", zap.Error(err))
		conn.Close()
		return
	}
	conn.SetReadDeadline(time.Time{})

	if msg.Type != proto.MsgAuth {
		s.logger.Warn("expected AUTH message", zap.String("got", msg.Type.String()))
		conn.Close()
		return
	}

	auth, err := proto.DecodeJSON[proto.AuthPayload](msg.Payload)
	if err != nil {
		s.logger.Warn("invalid auth payload", zap.Error(err))
		conn.Close()
		return
	}

	if !crypto.VerifyAuthToken(auth.ClientName, auth.Token, s.cipher.Key()) {
		s.logger.Warn("auth failed", zap.String("client", auth.ClientName))
		failMsg := proto.NewMessage(proto.MsgAuthFail, 0, nil)
		_ = writer.WriteMessage(failMsg)
		conn.Close()
		return
	}

	// Kick existing session with same name
	if existing := s.registry.GetClient(auth.ClientName); existing != nil {
		s.logger.Info("replacing existing session", zap.String("client", auth.ClientName))
		s.registry.RemoveClient(auth.ClientName)
	}

	session := &ClientSession{
		Name:        auth.ClientName,
		Conn:        conn,
		Writer:      writer,
		ConnectedAt: time.Now(),
		done:        make(chan struct{}),
	}
	s.registry.AddClient(session)
	s.logger.Info("client authenticated",
		zap.String("client", auth.ClientName),
		zap.String("addr", conn.RemoteAddr().String()))

	okMsg := proto.NewMessage(proto.MsgAuthOK, 0, nil)
	if err := writer.WriteMessage(okMsg); err != nil {
		s.logger.Error("write auth ok failed", zap.Error(err))
		s.registry.RemoveClient(auth.ClientName)
		return
	}

	// Push stored expose+forward rules to the client (in a goroutine to not block auth).
	go s.pushConfigToClient(auth.ClientName)

	defer func() {
		s.logger.Info("client disconnected", zap.String("client", auth.ClientName))
		s.registry.RemoveClient(auth.ClientName)
	}()

	for {
		select {
		case <-ctx.Done():
			return
		case <-session.done:
			return
		default:
		}

		msg, err := tunnel.ReadEncryptedMessage(br, s.cipher)
		if err != nil {
			s.logger.Debug("client read error",
				zap.String("client", auth.ClientName),
				zap.Error(err))
			return
		}

		s.handleMessage(auth.ClientName, writer, msg)
	}
}

func (s *Server) handleMessage(clientName string, writer *tunnel.ConnWriter, msg *proto.Message) {
	switch msg.Type {
	case proto.MsgRegister:
		s.handleRegister(clientName, msg)
	case proto.MsgOpenTunnel:
		s.handleOpenTunnel(clientName, writer, msg)
	case proto.MsgData, proto.MsgDataUDP:
		s.relay.ForwardData(clientName, msg)
	case proto.MsgClose:
		s.relay.ForwardClose(clientName, msg)
	case proto.MsgHeartbeat:
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgHeartbeat, 0, nil))
	case proto.MsgResponse:
		s.handleClientResponse(msg)
	default:
		s.logger.Warn("unknown message type",
			zap.String("client", clientName),
			zap.String("type", msg.Type.String()))
	}
}

func (s *Server) handleClientResponse(msg *proto.Message) {
	s.pendingMu.Lock()
	ch, ok := s.pendingReqs[msg.SessionID]
	if ok {
		delete(s.pendingReqs, msg.SessionID)
	}
	s.pendingMu.Unlock()

	if ok {
		select {
		case ch <- msg:
		default:
		}
	}
}

// forwardToClientAndWait sends a message to a client and waits for its response.
func (s *Server) forwardToClientAndWait(clientName string, msgType proto.MsgType, payload []byte) (*proto.Message, error) {
	client := s.registry.GetClient(clientName)
	if client == nil {
		return nil, fmt.Errorf("client %q not found", clientName)
	}

	reqID := atomic.AddUint32(&s.nextReqID, 1)
	ch := make(chan *proto.Message, 1)

	s.pendingMu.Lock()
	s.pendingReqs[reqID] = ch
	s.pendingMu.Unlock()

	defer func() {
		s.pendingMu.Lock()
		delete(s.pendingReqs, reqID)
		s.pendingMu.Unlock()
	}()

	if err := client.Writer.WriteMessage(proto.NewMessage(msgType, reqID, payload)); err != nil {
		return nil, fmt.Errorf("send to client: %w", err)
	}

	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()

	select {
	case resp := <-ch:
		return resp, nil
	case <-timer.C:
		return nil, fmt.Errorf("client response timeout")
	}
}

func (s *Server) handleRegister(clientName string, msg *proto.Message) {
	reg, err := proto.DecodeJSON[proto.RegisterPayload](msg.Payload)
	if err != nil {
		s.logger.Warn("invalid register payload", zap.Error(err))
		return
	}
	s.registry.UpdateServices(clientName, reg.Services)
	s.logger.Info("services registered",
		zap.String("client", clientName),
		zap.Int("count", len(reg.Services)))
}

func (s *Server) handleOpenTunnel(clientName string, writer *tunnel.ConnWriter, msg *proto.Message) {
	// msg.SessionID carries the source client's reqID; echo it back so the
	// client can match TunnelReady/TunnelFail to the correct pending request.
	reqID := msg.SessionID

	req, err := proto.DecodeJSON[proto.OpenTunnelPayload](msg.Payload)
	if err != nil {
		s.logger.Warn("invalid open tunnel payload", zap.Error(err))
		return
	}

	target := s.registry.GetClient(req.TargetClient)
	if target == nil {
		payload, _ := proto.EncodeJSON(&proto.TunnelFailPayload{
			Reason: fmt.Sprintf("client %q not found", req.TargetClient),
		})
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgTunnelFail, reqID, payload))
		return
	}

	hasService := false
	for _, svc := range target.Services {
		if svc.Name == req.TargetService {
			hasService = true
			break
		}
	}
	if !hasService {
		payload, _ := proto.EncodeJSON(&proto.TunnelFailPayload{
			Reason: fmt.Sprintf("service %q not found on client %q", req.TargetService, req.TargetClient),
		})
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgTunnelFail, reqID, payload))
		return
	}

	sessionID := s.registry.AllocateSession(clientName, req.TargetClient, req.TargetService, req.Protocol)

	// Notify target about incoming tunnel (use real sessionID for data routing).
	readyPayload, _ := proto.EncodeJSON(&proto.TunnelReadyPayload{
		SessionID:     sessionID,
		SourceClient:  clientName,
		TargetService: req.TargetService,
		Protocol:      req.Protocol,
	})
	if err := target.Writer.WriteMessage(proto.NewMessage(proto.MsgTunnelReady, sessionID, readyPayload)); err != nil {
		s.logger.Error("notify target failed", zap.Error(err))
		s.registry.RemoveTunnel(sessionID)
		payload, _ := proto.EncodeJSON(&proto.TunnelFailPayload{Reason: "target unreachable"})
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgTunnelFail, reqID, payload))
		return
	}

	// Notify source: use reqID so client matches the response, payload carries real sessionID.
	sourcePayload, _ := proto.EncodeJSON(&proto.TunnelReadyPayload{
		SessionID:     sessionID,
		SourceClient:  clientName,
		TargetService: req.TargetService,
		Protocol:      req.Protocol,
	})
	_ = writer.WriteMessage(proto.NewMessage(proto.MsgTunnelReady, reqID, sourcePayload))

	s.logger.Info("tunnel established",
		zap.Uint32("session", sessionID),
		zap.String("source", clientName),
		zap.String("target", req.TargetClient),
		zap.String("service", req.TargetService))
}

func (s *Server) serveUDP(ctx context.Context) error {
	addr, err := net.ResolveUDPAddr("udp", s.cfg.UDPAddr)
	if err != nil {
		return fmt.Errorf("resolve udp addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return fmt.Errorf("udp listen: %w", err)
	}
	defer conn.Close()
	s.logger.Info("UDP relay listening", zap.String("addr", s.cfg.UDPAddr))

	go func() {
		<-ctx.Done()
		conn.Close()
	}()

	buf := make([]byte, 65536)
	for {
		n, remoteAddr, err := conn.ReadFromUDP(buf)
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				s.logger.Error("udp read error", zap.Error(err))
				continue
			}
		}

		data := make([]byte, n)
		copy(data, buf[:n])
		go s.handleUDPPacket(conn, remoteAddr, data)
	}
}

func (s *Server) handleUDPPacket(conn *net.UDPConn, addr *net.UDPAddr, data []byte) {
	decrypted, err := s.cipher.Decrypt(data)
	if err != nil {
		s.logger.Debug("udp decrypt failed", zap.Error(err))
		return
	}

	msg, err := proto.ReadMessageFromBytes(decrypted)
	if err != nil {
		s.logger.Debug("udp parse failed", zap.Error(err))
		return
	}

	if msg.Type != proto.MsgDataUDP {
		return
	}

	t := s.registry.GetTunnel(msg.SessionID)
	if t == nil {
		return
	}

	// Forward via TCP control channel to the target
	var targetName string
	// We need to figure out who sent this; for UDP we embed sender info
	// For simplicity, relay UDP data through the TCP control channel
	targetName = t.TargetClient
	target := s.registry.GetClient(targetName)
	if target == nil {
		targetName = t.SourceClient
		target = s.registry.GetClient(targetName)
	}
	if target == nil {
		return
	}

	_ = target.Writer.WriteMessage(msg)
}

// serveAdmin is implemented in admin.go
func (s *Server) serveAdmin(ctx context.Context) error {
	ln, err := net.Listen("tcp", s.cfg.AdminAddr)
	if err != nil {
		return fmt.Errorf("admin listen: %w", err)
	}
	defer ln.Close()
	s.logger.Info("Admin API listening", zap.String("addr", s.cfg.AdminAddr))

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				continue
			}
		}
		go s.handleAdmin(conn)
	}
}

func (s *Server) handleAdmin(conn net.Conn) {
	defer conn.Close()
	writer := tunnel.NewConnWriter(conn, s.cipher, s.logger)
	br := tunnel.NewBufReader(conn)

	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	msg, err := tunnel.ReadEncryptedMessage(br, s.cipher)
	if err != nil {
		return
	}
	conn.SetReadDeadline(time.Time{})

	if msg.Type != proto.MsgAuth {
		return
	}

	auth, err := proto.DecodeJSON[proto.AuthPayload](msg.Payload)
	if err != nil {
		return
	}

	if !crypto.VerifyAuthToken(auth.ClientName, auth.Token, s.cipher.Key()) {
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgAuthFail, 0, nil))
		return
	}
	_ = writer.WriteMessage(proto.NewMessage(proto.MsgAuthOK, 0, nil))

	for {
		msg, err := tunnel.ReadEncryptedMessage(br, s.cipher)
		if err != nil {
			return
		}
		s.handleAdminMessage(writer, msg)
	}
}

func (s *Server) handleAdminMessage(writer *tunnel.ConnWriter, msg *proto.Message) {
	switch msg.Type {
	case proto.MsgListClients:
		s.handleAdminListClients(writer, msg)

	case proto.MsgListTunnels:
		tunnels := s.registry.ListTunnels()
		data, _ := json.Marshal(tunnels)
		resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
			Success: true,
			Message: fmt.Sprintf("%d active tunnels", len(tunnels)),
			Data:    data,
		})
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp))

	case proto.MsgKickClient:
		kick, err := proto.DecodeJSON[proto.KickPayload](msg.Payload)
		if err != nil {
			s.adminReply(writer, false, "invalid payload", nil)
			return
		}
		client := s.registry.GetClient(kick.ClientName)
		if client == nil {
			s.adminReply(writer, false, fmt.Sprintf("client %q not found", kick.ClientName), nil)
			return
		}
		s.registry.RemoveClient(kick.ClientName)
		s.adminReply(writer, true, fmt.Sprintf("client %q kicked", kick.ClientName), nil)

	case proto.MsgAddForward:
		s.handleAdminAddForward(writer, msg)

	case proto.MsgRemoveForward:
		s.handleAdminRemoveForward(writer, msg)

	case proto.MsgListForwards:
		s.handleAdminListForwards(writer, msg)

	case proto.MsgAddExpose:
		s.handleAdminAddExpose(writer, msg)

	case proto.MsgRemoveExpose:
		s.handleAdminRemoveExpose(writer, msg)

	case proto.MsgListExpose:
		s.handleAdminListExpose(writer, msg)

	}
}

// pushConfigToClient sends stored expose+forward rules to a client if it is online.
func (s *Server) pushConfigToClient(clientName string) {
	rules := s.store.GetClientRules(clientName)
	if rules == nil || (len(rules.Expose) == 0 && len(rules.Forward) == 0) {
		return
	}
	client := s.registry.GetClient(clientName)
	if client == nil {
		return
	}
	cfgPayload, _ := proto.EncodeJSON(&proto.PushConfigPayload{
		Expose:  rules.Expose,
		Forward: rules.Forward,
	})
	if err := client.Writer.WriteMessage(proto.NewMessage(proto.MsgPushConfig, 0, cfgPayload)); err != nil {
		s.logger.Warn("push config failed", zap.String("client", clientName), zap.Error(err))
	} else {
		s.logger.Info("pushed stored config to client",
			zap.String("client", clientName),
			zap.Int("expose", len(rules.Expose)),
			zap.Int("forward", len(rules.Forward)))
	}
}

func (s *Server) adminReply(writer *tunnel.ConnWriter, success bool, message string, data json.RawMessage) {
	resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
		Success: success,
		Message: message,
		Data:    data,
	})
	_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp))
}

func (s *Server) handleAdminAddForward(writer *tunnel.ConnWriter, msg *proto.Message) {
	req, err := proto.DecodeJSON[proto.AddForwardPayload](msg.Payload)
	if err != nil {
		s.adminReply(writer, false, "invalid payload", nil)
		return
	}

	// Persist to store first so it survives reconnects.
	fwdInfo := proto.ForwardInfo{
		ClientName:    req.ClientName,
		ListenAddr:    req.ListenAddr,
		RemoteClient:  req.RemoteClient,
		RemoteService: req.RemoteService,
		Protocol:      req.Protocol,
	}
	if err := s.store.AddForward(req.ClientName, fwdInfo); err != nil {
		s.logger.Warn("store forward failed", zap.Error(err))
	}

	// If client is online, apply immediately.
	resp, err := s.forwardToClientAndWait(req.ClientName, proto.MsgAddForward, msg.Payload)
	if err != nil {
		// Client offline is OK — rule is stored and will be pushed on next connect.
		s.adminReply(writer, true, fmt.Sprintf("forward rule saved (client offline: %s)", err.Error()), nil)
		return
	}
	_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp.Payload))
}

func (s *Server) handleAdminRemoveForward(writer *tunnel.ConnWriter, msg *proto.Message) {
	req, err := proto.DecodeJSON[proto.RemoveForwardPayload](msg.Payload)
	if err != nil {
		s.adminReply(writer, false, "invalid payload", nil)
		return
	}

	if err := s.store.RemoveForward(req.ClientName, req.ListenAddr); err != nil {
		s.logger.Warn("store remove forward failed", zap.Error(err))
	}

	resp, err := s.forwardToClientAndWait(req.ClientName, proto.MsgRemoveForward, msg.Payload)
	if err != nil {
		s.adminReply(writer, true, fmt.Sprintf("forward rule removed from store (client offline: %s)", err.Error()), nil)
		return
	}
	_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp.Payload))
}

func (s *Server) handleAdminListClients(writer *tunnel.ConnWriter, msg *proto.Message) {
	var req proto.ListClientsPayload
	if len(msg.Payload) > 0 {
		if p, err := proto.DecodeJSON[proto.ListClientsPayload](msg.Payload); err == nil {
			req = *p
		}
	}

	clients := s.registry.ListClients()

	if !req.SpeedTest {
		data, _ := json.Marshal(clients)
		resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
			Success: true,
			Message: fmt.Sprintf("%d clients connected", len(clients)),
			Data:    data,
		})
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp))
		return
	}

	// Run speed tests concurrently.
	const probeSize = 512 * 1024
	probeData := make([]byte, probeSize)
	for i := range probeData {
		probeData[i] = byte(i & 0xff)
	}

	results := make([]proto.ClientSpeedInfo, len(clients))
	for i, c := range clients {
		results[i] = proto.ClientSpeedInfo{ClientInfo: c, RTTMs: -1, ThroughputKBps: -1}
	}

	var wg sync.WaitGroup
	for i := range results {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			t0 := time.Now()
			resp, err := s.forwardToClientAndWait(results[i].Name, proto.MsgSpeedTest, probeData)
			rtt := time.Since(t0)
			if err != nil {
				return
			}
			result, err := proto.DecodeJSON[proto.ResponsePayload](resp.Payload)
			if err != nil || !result.Success {
				return
			}
			speedResult, err := proto.DecodeJSON[proto.SpeedResultPayload](result.Data)
			if err != nil {
				return
			}
			results[i].RTTMs = rtt.Milliseconds()
			// Throughput = probe size / RTT (server-side round-trip measurement)
			if rtt.Milliseconds() > 0 {
				results[i].ThroughputKBps = int64(probeSize/1024) * 1000 / rtt.Milliseconds()
			}
			_ = speedResult
		}()
	}
	wg.Wait()

	data, _ := json.Marshal(results)
	resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
		Success: true,
		Message: fmt.Sprintf("%d clients connected", len(clients)),
		Data:    data,
	})
	_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp))
}

func (s *Server) handleAdminAddExpose(writer *tunnel.ConnWriter, msg *proto.Message) {
	req, err := proto.DecodeJSON[proto.AddExposePayload](msg.Payload)
	if err != nil {
		s.adminReply(writer, false, "invalid payload", nil)
		return
	}
	if req.Rule.Protocol == "" {
		req.Rule.Protocol = "tcp"
	}
	if err := s.store.AddExpose(req.ClientName, req.Rule); err != nil {
		s.adminReply(writer, false, fmt.Sprintf("store error: %s", err), nil)
		return
	}

	// Push updated config to online client asynchronously (don't block admin response).
	go s.pushConfigToClient(req.ClientName)
	s.adminReply(writer, true, fmt.Sprintf("expose rule %q saved for client %q", req.Rule.Name, req.ClientName), nil)
}

func (s *Server) handleAdminRemoveExpose(writer *tunnel.ConnWriter, msg *proto.Message) {
	req, err := proto.DecodeJSON[proto.RemoveExposePayload](msg.Payload)
	if err != nil {
		s.adminReply(writer, false, "invalid payload", nil)
		return
	}
	if err := s.store.RemoveExpose(req.ClientName, req.ServiceName); err != nil {
		s.adminReply(writer, false, fmt.Sprintf("store error: %s", err), nil)
		return
	}

	go s.pushConfigToClient(req.ClientName)
	s.adminReply(writer, true, fmt.Sprintf("expose rule %q removed from client %q", req.ServiceName, req.ClientName), nil)
}

func (s *Server) handleAdminListExpose(writer *tunnel.ConnWriter, msg *proto.Message) {
	var clientFilter string
	if len(msg.Payload) > 0 {
		if req, err := proto.DecodeJSON[proto.KickPayload](msg.Payload); err == nil {
			clientFilter = req.ClientName
		}
	}
	entries := s.store.ListExpose(clientFilter)
	data, _ := json.Marshal(entries)
	s.adminReply(writer, true, fmt.Sprintf("%d expose rules", len(entries)), data)
}

func (s *Server) handleAdminListForwards(writer *tunnel.ConnWriter, msg *proto.Message) {
	// If a specific client is requested, forward to that client
	if len(msg.Payload) > 0 {
		req, err := proto.DecodeJSON[proto.KickPayload](msg.Payload)
		if err == nil && req.ClientName != "" {
			resp, err := s.forwardToClientAndWait(req.ClientName, proto.MsgListForwards, nil)
			if err != nil {
				s.adminReply(writer, false, err.Error(), nil)
				return
			}
			_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp.Payload))
			return
		}
	}

	// Otherwise, query all clients and aggregate
	var allForwards []proto.ForwardInfo
	clients := s.registry.ListClients()
	for _, c := range clients {
		resp, err := s.forwardToClientAndWait(c.Name, proto.MsgListForwards, nil)
		if err != nil {
			continue
		}
		result, err := proto.DecodeJSON[proto.ResponsePayload](resp.Payload)
		if err != nil || !result.Success {
			continue
		}
		var forwards []proto.ForwardInfo
		if len(result.Data) > 0 {
			_ = json.Unmarshal(result.Data, &forwards)
			allForwards = append(allForwards, forwards...)
		}
	}

	data, _ := json.Marshal(allForwards)
	s.adminReply(writer, true, fmt.Sprintf("%d active forwards", len(allForwards)), data)
}
