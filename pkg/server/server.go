package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
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
	logger   *zap.Logger
}

func New(cfg *config.ServerConfig, logger *zap.Logger) *Server {
	salt := []byte("clienthub-fixed-salt-v1")
	cipher := crypto.NewCipherFromPassword(cfg.Secret, salt)
	reg := NewRegistry()
	return &Server{
		cfg:      cfg,
		cipher:   cipher,
		registry: reg,
		relay:    NewRelay(reg, logger),
		logger:   logger,
	}
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

	// Read auth message with timeout
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	msg, err := tunnel.ReadEncryptedMessage(conn, s.cipher)
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

		msg, err := tunnel.ReadEncryptedMessage(conn, s.cipher)
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
		// Echo heartbeat back
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgHeartbeat, 0, nil))
	default:
		s.logger.Warn("unknown message type",
			zap.String("client", clientName),
			zap.String("type", msg.Type.String()))
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
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgTunnelFail, 0, payload))
		return
	}

	// Check target has the requested service
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
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgTunnelFail, 0, payload))
		return
	}

	sessionID := s.registry.AllocateSession(clientName, req.TargetClient, req.TargetService, req.Protocol)

	// Notify target about incoming tunnel
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
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgTunnelFail, 0, payload))
		return
	}

	// Notify source that tunnel is ready
	sourcePayload, _ := proto.EncodeJSON(&proto.TunnelReadyPayload{
		SessionID:     sessionID,
		SourceClient:  clientName,
		TargetService: req.TargetService,
		Protocol:      req.Protocol,
	})
	_ = writer.WriteMessage(proto.NewMessage(proto.MsgTunnelReady, sessionID, sourcePayload))

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

	// Auth
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))
	msg, err := tunnel.ReadEncryptedMessage(conn, s.cipher)
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

	// Process admin commands
	for {
		msg, err := tunnel.ReadEncryptedMessage(conn, s.cipher)
		if err != nil {
			return
		}
		s.handleAdminMessage(writer, msg)
	}
}

func (s *Server) handleAdminMessage(writer *tunnel.ConnWriter, msg *proto.Message) {
	switch msg.Type {
	case proto.MsgListClients:
		clients := s.registry.ListClients()
		data, _ := json.Marshal(clients)
		resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
			Success: true,
			Message: fmt.Sprintf("%d clients connected", len(clients)),
			Data:    data,
		})
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp))

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
			resp, _ := proto.EncodeJSON(&proto.ResponsePayload{Success: false, Message: "invalid payload"})
			_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp))
			return
		}
		client := s.registry.GetClient(kick.ClientName)
		if client == nil {
			resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
				Success: false,
				Message: fmt.Sprintf("client %q not found", kick.ClientName),
			})
			_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp))
			return
		}
		s.registry.RemoveClient(kick.ClientName)
		resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
			Success: true,
			Message: fmt.Sprintf("client %q kicked", kick.ClientName),
		})
		_ = writer.WriteMessage(proto.NewMessage(proto.MsgResponse, 0, resp))
	}
}
