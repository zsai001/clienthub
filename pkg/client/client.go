package client

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/cltx/clienthub/config"
	"github.com/cltx/clienthub/pkg/crypto"
	"github.com/cltx/clienthub/pkg/proto"
	"github.com/cltx/clienthub/pkg/tunnel"
	"go.uber.org/zap"
)

type Client struct {
	cfg    *config.ClientConfig
	cipher *crypto.Cipher
	conn   net.Conn
	writer *tunnel.ConnWriter
	logger *zap.Logger
	ctx    context.Context

	mu       sync.RWMutex
	tunnels  map[uint32]*localTunnel
	pending  map[string]chan uint32 // key: "targetClient/targetService" -> sessionID
	forwards map[string]*activeForward // key: listenAddr -> active forward proxy
}

type localTunnel struct {
	sessionID uint32
	localConn net.Conn
	protocol  string
}

type activeForward struct {
	ListenAddr    string
	RemoteClient  string
	RemoteService string
	Protocol      string
	cancel        context.CancelFunc
}

func New(cfg *config.ClientConfig, logger *zap.Logger) *Client {
	salt := []byte("clienthub-fixed-salt-v1")
	cipher := crypto.NewCipherFromPassword(cfg.Secret, salt)
	return &Client{
		cfg:      cfg,
		cipher:   cipher,
		logger:   logger,
		tunnels:  make(map[uint32]*localTunnel),
		pending:  make(map[string]chan uint32),
		forwards: make(map[string]*activeForward),
	}
}

func (c *Client) Run(ctx context.Context) error {
	conn, err := net.DialTimeout("tcp", c.cfg.ServerAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}
	c.conn = conn
	c.writer = tunnel.NewConnWriter(conn, c.cipher, c.logger)

	defer conn.Close()

	if err := c.authenticate(); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	c.logger.Info("authenticated with server", zap.String("server", c.cfg.ServerAddr))

	if err := c.registerServices(); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// Start local proxy listeners for forward rules
	for _, fwd := range c.cfg.Forward {
		fwd := fwd
		go c.startForwardProxy(ctx, fwd)
	}

	// Start heartbeat
	go c.heartbeatLoop(ctx)

	// Read messages from server
	return c.readLoop(ctx)
}

func (c *Client) authenticate() error {
	token := crypto.ComputeAuthToken(c.cfg.ClientName, c.cipher.Key())
	payload, err := proto.EncodeJSON(&proto.AuthPayload{
		ClientName: c.cfg.ClientName,
		Token:      token,
	})
	if err != nil {
		return err
	}

	msg := proto.NewMessage(proto.MsgAuth, 0, payload)
	if err := c.writer.WriteMessage(msg); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	resp, err := tunnel.ReadEncryptedMessage(c.conn, c.cipher)
	if err != nil {
		return fmt.Errorf("read auth response: %w", err)
	}

	if resp.Type == proto.MsgAuthFail {
		return proto.ErrAuthFailed
	}
	if resp.Type != proto.MsgAuthOK {
		return fmt.Errorf("unexpected response: %s", resp.Type)
	}
	return nil
}

func (c *Client) registerServices() error {
	if len(c.cfg.Expose) == 0 {
		return nil
	}

	services := make([]proto.ServiceInfo, len(c.cfg.Expose))
	for i, exp := range c.cfg.Expose {
		_, portStr, _ := net.SplitHostPort(exp.LocalAddr)
		port := 0
		fmt.Sscanf(portStr, "%d", &port)
		services[i] = proto.ServiceInfo{
			Name:     exp.Name,
			Protocol: exp.Protocol,
			Port:     port,
		}
	}

	payload, err := proto.EncodeJSON(&proto.RegisterPayload{Services: services})
	if err != nil {
		return err
	}

	return c.writer.WriteMessage(proto.NewMessage(proto.MsgRegister, 0, payload))
}

func (c *Client) heartbeatLoop(ctx context.Context) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			_ = c.writer.WriteMessage(proto.NewMessage(proto.MsgHeartbeat, 0, nil))
		}
	}
}

func (c *Client) readLoop(ctx context.Context) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		msg, err := tunnel.ReadEncryptedMessage(c.conn, c.cipher)
		if err != nil {
			return fmt.Errorf("read from server: %w", err)
		}

		switch msg.Type {
		case proto.MsgTunnelReady:
			c.handleTunnelReady(msg)
		case proto.MsgTunnelFail:
			c.handleTunnelFail(msg)
		case proto.MsgData, proto.MsgDataUDP:
			c.handleData(msg)
		case proto.MsgClose:
			c.handleClose(msg)
		case proto.MsgHeartbeat:
			// ignore heartbeat echo
		default:
			c.logger.Debug("unhandled message", zap.String("type", msg.Type.String()))
		}
	}
}

func (c *Client) handleTunnelReady(msg *proto.Message) {
	ready, err := proto.DecodeJSON[proto.TunnelReadyPayload](msg.Payload)
	if err != nil {
		c.logger.Warn("invalid tunnel ready payload", zap.Error(err))
		return
	}

	if ready.SourceClient == c.cfg.ClientName {
		c.mu.RLock()
		for _, ch := range c.pending {
			select {
			case ch <- ready.SessionID:
				c.logger.Info("tunnel ready for forward",
					zap.Uint32("session", ready.SessionID))
			default:
			}
		}
		c.mu.RUnlock()
	} else {
		// We are the target; connect to local service
		go c.handleIncomingTunnel(ready)
	}
}

func (c *Client) handleIncomingTunnel(ready *proto.TunnelReadyPayload) {
	// Find the matching exposed service
	var localAddr string
	for _, exp := range c.cfg.Expose {
		if exp.Name == ready.TargetService {
			localAddr = exp.LocalAddr
			break
		}
	}
	if localAddr == "" {
		c.logger.Warn("no local service for tunnel",
			zap.String("service", ready.TargetService))
		_ = c.writer.WriteMessage(proto.NewMessage(proto.MsgClose, ready.SessionID, nil))
		return
	}

	localConn, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
	if err != nil {
		c.logger.Error("connect to local service failed",
			zap.String("addr", localAddr),
			zap.Error(err))
		_ = c.writer.WriteMessage(proto.NewMessage(proto.MsgClose, ready.SessionID, nil))
		return
	}

	c.mu.Lock()
	c.tunnels[ready.SessionID] = &localTunnel{
		sessionID: ready.SessionID,
		localConn: localConn,
		protocol:  ready.Protocol,
	}
	c.mu.Unlock()

	c.logger.Info("incoming tunnel connected to local service",
		zap.Uint32("session", ready.SessionID),
		zap.String("service", ready.TargetService),
		zap.String("local", localAddr))

	// Read from local service and forward to server
	go c.forwardLocalToServer(ready.SessionID, localConn)
}

func (c *Client) handleTunnelFail(msg *proto.Message) {
	fail, err := proto.DecodeJSON[proto.TunnelFailPayload](msg.Payload)
	if err != nil {
		c.logger.Warn("invalid tunnel fail payload", zap.Error(err))
		return
	}
	c.logger.Warn("tunnel open failed", zap.String("reason", fail.Reason))

	// Notify all pending (simple approach)
	c.mu.Lock()
	for _, ch := range c.pending {
		select {
		case ch <- 0:
		default:
		}
	}
	c.mu.Unlock()
}

func (c *Client) handleData(msg *proto.Message) {
	c.mu.RLock()
	t, ok := c.tunnels[msg.SessionID]
	c.mu.RUnlock()
	if !ok {
		return
	}

	if t.localConn != nil {
		_, err := t.localConn.Write(msg.Payload)
		if err != nil {
			c.logger.Debug("write to local failed",
				zap.Uint32("session", msg.SessionID),
				zap.Error(err))
			c.closeTunnel(msg.SessionID)
		}
	}
}

func (c *Client) handleClose(msg *proto.Message) {
	c.closeTunnel(msg.SessionID)
}

func (c *Client) closeTunnel(sessionID uint32) {
	c.mu.Lock()
	t, ok := c.tunnels[sessionID]
	if ok {
		delete(c.tunnels, sessionID)
	}
	c.mu.Unlock()

	if ok && t.localConn != nil {
		t.localConn.Close()
	}
}

func (c *Client) forwardLocalToServer(sessionID uint32, localConn net.Conn) {
	buf := make([]byte, 32*1024)
	for {
		n, err := localConn.Read(buf)
		if n > 0 {
			data := make([]byte, n)
			copy(data, buf[:n])
			msg := proto.NewMessage(proto.MsgData, sessionID, data)
			if writeErr := c.writer.WriteMessage(msg); writeErr != nil {
				c.logger.Debug("write to server failed",
					zap.Uint32("session", sessionID),
					zap.Error(writeErr))
				break
			}
		}
		if err != nil {
			break
		}
	}

	_ = c.writer.WriteMessage(proto.NewMessage(proto.MsgClose, sessionID, nil))
	c.closeTunnel(sessionID)
}
