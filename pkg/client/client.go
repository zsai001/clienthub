package client

import (
	"bufio"
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
	logger *zap.Logger

	// protected by mu
	mu       sync.RWMutex
	conn     net.Conn
	writer   *tunnel.ConnWriter
	tunnels  map[uint32]*localTunnel
	pending  map[uint32]chan uint32 // reqID -> sessionID response channel
	forwards map[string]*activeForward

	nextReqID uint32 // atomic
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
		pending:  make(map[uint32]chan uint32),
		forwards: make(map[string]*activeForward),
	}
}

// Run connects to the server and maintains the connection with auto-reconnect.
func (c *Client) Run(ctx context.Context) error {
	backoff := 2 * time.Second
	const maxBackoff = 60 * time.Second

	for {
		err := c.runOnce(ctx)
		if err == nil || ctx.Err() != nil {
			return ctx.Err()
		}
		c.logger.Warn("disconnected from server, reconnecting",
			zap.Error(err),
			zap.Duration("backoff", backoff))

		// Drain pending tunnel requests so callers unblock immediately.
		c.mu.Lock()
		for id, ch := range c.pending {
			select {
			case ch <- 0:
			default:
			}
			delete(c.pending, id)
		}
		// Close all active tunnels.
		for id, t := range c.tunnels {
			if t.localConn != nil {
				t.localConn.Close()
			}
			delete(c.tunnels, id)
		}
		c.mu.Unlock()

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
		backoff *= 2
		if backoff > maxBackoff {
			backoff = maxBackoff
		}
	}
}

func (c *Client) runOnce(ctx context.Context) error {
	conn, err := net.DialTimeout("tcp", c.cfg.ServerAddr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("connect to server: %w", err)
	}

	writer := tunnel.NewConnWriter(conn, c.cipher, c.logger)
	br := tunnel.NewBufReader(conn)

	c.mu.Lock()
	c.conn = conn
	c.writer = writer
	c.mu.Unlock()

	defer func() {
		conn.Close()
		c.mu.Lock()
		c.conn = nil
		c.writer = nil
		c.mu.Unlock()
	}()

	if err := c.authenticate(conn, writer, br); err != nil {
		return fmt.Errorf("auth: %w", err)
	}
	c.logger.Info("authenticated with server", zap.String("server", c.cfg.ServerAddr))

	if err := c.registerServices(writer); err != nil {
		return fmt.Errorf("register: %w", err)
	}

	// Re-establish static forward listeners (only on first connect; they survive reconnects).
	c.mu.RLock()
	noForwards := len(c.forwards) == 0
	c.mu.RUnlock()
	if noForwards {
		for _, fwd := range c.cfg.Forward {
			fwd := fwd
			if err := c.addForward(ctx, fwd.ListenAddr, fwd.RemoteClient, fwd.RemoteService, fwd.Protocol); err != nil {
				c.logger.Warn("static forward failed", zap.String("listen", fwd.ListenAddr), zap.Error(err))
			}
		}
	}

	go c.heartbeatLoop(ctx, writer)

	return c.readLoop(ctx, br)
}

func (c *Client) authenticate(conn net.Conn, writer *tunnel.ConnWriter, br *bufio.Reader) error {
	token := crypto.ComputeAuthToken(c.cfg.ClientName, c.cipher.Key())
	payload, err := proto.EncodeJSON(&proto.AuthPayload{
		ClientName: c.cfg.ClientName,
		Token:      token,
	})
	if err != nil {
		return err
	}

	if err := writer.WriteMessage(proto.NewMessage(proto.MsgAuth, 0, payload)); err != nil {
		return fmt.Errorf("send auth: %w", err)
	}

	resp, err := tunnel.ReadEncryptedMessage(br, c.cipher)
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

func (c *Client) registerServices(writer *tunnel.ConnWriter) error {
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
	return writer.WriteMessage(proto.NewMessage(proto.MsgRegister, 0, payload))
}

func (c *Client) heartbeatLoop(ctx context.Context, writer *tunnel.ConnWriter) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if err := writer.WriteMessage(proto.NewMessage(proto.MsgHeartbeat, 0, nil)); err != nil {
				return
			}
		}
	}
}

func (c *Client) readLoop(ctx context.Context, br *bufio.Reader) error {
	for {
		select {
		case <-ctx.Done():
			return nil
		default:
		}

		msg, err := tunnel.ReadEncryptedMessage(br, c.cipher)
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
		case proto.MsgAddForward:
			c.handleAddForward(ctx, msg)
		case proto.MsgRemoveForward:
			c.handleRemoveForward(msg)
		case proto.MsgListForwards:
			c.handleListForwards(msg)
		case proto.MsgPushConfig:
			c.handlePushConfig(ctx, msg)
		case proto.MsgSpeedTest:
			c.handleSpeedTest(msg)
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
		// msg.SessionID is the reqID we sent in MsgOpenTunnel; ready.SessionID is the real tunnel ID.
		c.mu.RLock()
		ch, ok := c.pending[msg.SessionID]
		c.mu.RUnlock()
		if ok {
			select {
			case ch <- ready.SessionID:
			default:
			}
		}
	} else {
		// We are the target; connect to local service.
		go c.handleIncomingTunnel(ready)
	}
}

func (c *Client) handleTunnelFail(msg *proto.Message) {
	fail, err := proto.DecodeJSON[proto.TunnelFailPayload](msg.Payload)
	if err != nil {
		c.logger.Warn("invalid tunnel fail payload", zap.Error(err))
		return
	}
	c.logger.Warn("tunnel open failed", zap.String("reason", fail.Reason))

	// msg.SessionID is the reqID we sent; signal failure with sessionID=0.
	c.mu.RLock()
	ch, ok := c.pending[msg.SessionID]
	c.mu.RUnlock()
	if ok {
		select {
		case ch <- 0:
		default:
		}
	}
}

func (c *Client) handleIncomingTunnel(ready *proto.TunnelReadyPayload) {
	var localAddr string
	for _, exp := range c.cfg.Expose {
		if exp.Name == ready.TargetService {
			localAddr = exp.LocalAddr
			break
		}
	}
	if localAddr == "" {
		c.logger.Warn("no local service for tunnel", zap.String("service", ready.TargetService))
		c.mu.RLock()
		w := c.writer
		c.mu.RUnlock()
		if w != nil {
			_ = w.WriteMessage(proto.NewMessage(proto.MsgClose, ready.SessionID, nil))
		}
		return
	}

	localConn, err := net.DialTimeout("tcp", localAddr, 5*time.Second)
	if err != nil {
		c.logger.Error("connect to local service failed",
			zap.String("addr", localAddr), zap.Error(err))
		c.mu.RLock()
		w := c.writer
		c.mu.RUnlock()
		if w != nil {
			_ = w.WriteMessage(proto.NewMessage(proto.MsgClose, ready.SessionID, nil))
		}
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

	go c.forwardLocalToServer(ready.SessionID, localConn)
}

func (c *Client) handleData(msg *proto.Message) {
	c.mu.RLock()
	t, ok := c.tunnels[msg.SessionID]
	c.mu.RUnlock()
	if !ok {
		return
	}
	if t.localConn != nil {
		if _, err := t.localConn.Write(msg.Payload); err != nil {
			c.logger.Debug("write to local failed",
				zap.Uint32("session", msg.SessionID), zap.Error(err))
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
			c.mu.RLock()
			w := c.writer
			c.mu.RUnlock()
			if w == nil {
				break
			}
			if writeErr := w.WriteMessage(proto.NewMessage(proto.MsgData, sessionID, data)); writeErr != nil {
				c.logger.Debug("write to server failed",
					zap.Uint32("session", sessionID), zap.Error(writeErr))
				break
			}
		}
		if err != nil {
			break
		}
	}

	c.mu.RLock()
	w := c.writer
	c.mu.RUnlock()
	if w != nil {
		_ = w.WriteMessage(proto.NewMessage(proto.MsgClose, sessionID, nil))
	}
	c.closeTunnel(sessionID)
}

func (c *Client) addForward(ctx context.Context, listenAddr, remoteClient, remoteService, protocol string) error {
	c.mu.Lock()
	if _, exists := c.forwards[listenAddr]; exists {
		c.mu.Unlock()
		return fmt.Errorf("forward already exists on %s", listenAddr)
	}
	c.mu.Unlock()

	if protocol == "" {
		protocol = "tcp"
	}

	ln, err := net.Listen("tcp", listenAddr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", listenAddr, err)
	}

	fwdCtx, cancel := context.WithCancel(ctx)
	fwd := &activeForward{
		ListenAddr:    listenAddr,
		RemoteClient:  remoteClient,
		RemoteService: remoteService,
		Protocol:      protocol,
		cancel:        cancel,
	}

	c.mu.Lock()
	c.forwards[listenAddr] = fwd
	c.mu.Unlock()

	go c.runForwardProxy(fwdCtx, ln, fwd)
	return nil
}

func (c *Client) removeForward(listenAddr string) error {
	c.mu.Lock()
	fwd, ok := c.forwards[listenAddr]
	if !ok {
		c.mu.Unlock()
		return fmt.Errorf("no forward on %s", listenAddr)
	}
	delete(c.forwards, listenAddr)
	c.mu.Unlock()
	fwd.cancel()
	return nil
}

func (c *Client) listForwards() []proto.ForwardInfo {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result := make([]proto.ForwardInfo, 0, len(c.forwards))
	for _, fwd := range c.forwards {
		result = append(result, proto.ForwardInfo{
			ClientName:    c.cfg.ClientName,
			ListenAddr:    fwd.ListenAddr,
			RemoteClient:  fwd.RemoteClient,
			RemoteService: fwd.RemoteService,
			Protocol:      fwd.Protocol,
		})
	}
	return result
}

func (c *Client) handleAddForward(ctx context.Context, msg *proto.Message) {
	req, err := proto.DecodeJSON[proto.AddForwardPayload](msg.Payload)
	if err != nil {
		c.sendResponse(msg.SessionID, false, "invalid payload")
		return
	}
	if err := c.addForward(ctx, req.ListenAddr, req.RemoteClient, req.RemoteService, req.Protocol); err != nil {
		c.sendResponse(msg.SessionID, false, err.Error())
		return
	}
	c.logger.Info("dynamic forward added",
		zap.String("listen", req.ListenAddr),
		zap.String("target", req.RemoteClient+"/"+req.RemoteService))
	c.sendResponse(msg.SessionID, true, fmt.Sprintf("forward %s -> %s/%s started", req.ListenAddr, req.RemoteClient, req.RemoteService))
}

func (c *Client) handleRemoveForward(msg *proto.Message) {
	req, err := proto.DecodeJSON[proto.RemoveForwardPayload](msg.Payload)
	if err != nil {
		c.sendResponse(msg.SessionID, false, "invalid payload")
		return
	}
	if err := c.removeForward(req.ListenAddr); err != nil {
		c.sendResponse(msg.SessionID, false, err.Error())
		return
	}
	c.logger.Info("dynamic forward removed", zap.String("listen", req.ListenAddr))
	c.sendResponse(msg.SessionID, true, fmt.Sprintf("forward on %s removed", req.ListenAddr))
}

func (c *Client) handleListForwards(msg *proto.Message) {
	forwards := c.listForwards()
	data, _ := proto.EncodeJSON(forwards)
	resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
		Success: true,
		Message: fmt.Sprintf("%d active forwards", len(forwards)),
		Data:    data,
	})
	c.mu.RLock()
	w := c.writer
	c.mu.RUnlock()
	if w != nil {
		_ = w.WriteMessage(proto.NewMessage(proto.MsgResponse, msg.SessionID, resp))
	}
}

func (c *Client) sendResponse(sessionID uint32, success bool, message string) {
	resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
		Success: success,
		Message: message,
	})
	c.mu.RLock()
	w := c.writer
	c.mu.RUnlock()
	if w != nil {
		_ = w.WriteMessage(proto.NewMessage(proto.MsgResponse, sessionID, resp))
	}
}

// handlePushConfig applies server-pushed expose and forward rules.
func (c *Client) handlePushConfig(ctx context.Context, msg *proto.Message) {
	cfg, err := proto.DecodeJSON[proto.PushConfigPayload](msg.Payload)
	if err != nil {
		c.logger.Warn("invalid push config payload", zap.Error(err))
		return
	}

	// Update expose list (used by handleIncomingTunnel).
	c.mu.Lock()
	c.cfg.Expose = make([]config.ExposeService, len(cfg.Expose))
	for i, e := range cfg.Expose {
		c.cfg.Expose[i] = config.ExposeService{
			Name:      e.Name,
			LocalAddr: e.LocalAddr,
			Protocol:  e.Protocol,
		}
	}
	c.mu.Unlock()

	// Register updated services with server.
	c.mu.RLock()
	w := c.writer
	c.mu.RUnlock()
	if w != nil {
		_ = c.registerServices(w)
	}

	// Apply forward rules: add new ones, remove stale ones.
	desired := make(map[string]proto.ForwardInfo)
	for _, f := range cfg.Forward {
		desired[f.ListenAddr] = f
	}

	c.mu.RLock()
	existing := make(map[string]struct{}, len(c.forwards))
	for addr := range c.forwards {
		existing[addr] = struct{}{}
	}
	c.mu.RUnlock()

	// Remove forwards no longer in desired set.
	for addr := range existing {
		if _, ok := desired[addr]; !ok {
			if err := c.removeForward(addr); err != nil {
				c.logger.Warn("remove stale forward failed", zap.String("listen", addr), zap.Error(err))
			}
		}
	}
	// Add new forwards.
	for addr, f := range desired {
		if _, ok := existing[addr]; !ok {
			proto := f.Protocol
			if proto == "" {
				proto = "tcp"
			}
			if err := c.addForward(ctx, addr, f.RemoteClient, f.RemoteService, proto); err != nil {
				c.logger.Warn("add pushed forward failed", zap.String("listen", addr), zap.Error(err))
			}
		}
	}

	c.logger.Info("applied pushed config",
		zap.Int("expose", len(cfg.Expose)),
		zap.Int("forward", len(cfg.Forward)))
}

// handleSpeedTest responds to a server speed probe with timing information.
func (c *Client) handleSpeedTest(msg *proto.Message) {
	recvBytes := int64(len(msg.Payload))
	// We measure from when the message was fully received (now) back to an
	// approximated start. Since we can't timestamp the first byte here without
	// deeper instrumentation, we report recvDurationMs=0 and let the server
	// use RTT as the primary metric. The throughput is computed server-side
	// from RTT and payload size.
	result, _ := proto.EncodeJSON(&proto.SpeedResultPayload{
		RecvBytes:      recvBytes,
		RecvDurationMs: 0, // server uses round-trip time instead
	})
	resp, _ := proto.EncodeJSON(&proto.ResponsePayload{
		Success: true,
		Message: "speed result",
		Data:    result,
	})
	c.mu.RLock()
	w := c.writer
	c.mu.RUnlock()
	if w != nil {
		_ = w.WriteMessage(proto.NewMessage(proto.MsgResponse, msg.SessionID, resp))
	}
}
