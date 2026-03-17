package client

import (
	"context"
	"fmt"
	"net"
	"time"

	"github.com/cltx/clienthub/config"
	"github.com/cltx/clienthub/pkg/proto"
	"go.uber.org/zap"
)

func (c *Client) startForwardProxy(ctx context.Context, fwd config.ForwardRule) {
	ln, err := net.Listen("tcp", fwd.ListenAddr)
	if err != nil {
		c.logger.Error("forward proxy listen failed",
			zap.String("addr", fwd.ListenAddr),
			zap.Error(err))
		return
	}
	defer ln.Close()

	c.logger.Info("forward proxy listening",
		zap.String("listen", fwd.ListenAddr),
		zap.String("remote", fmt.Sprintf("%s/%s", fwd.RemoteClient, fwd.RemoteService)),
		zap.String("protocol", fwd.Protocol))

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return
			default:
				c.logger.Debug("proxy accept error", zap.Error(err))
				continue
			}
		}
		go c.handleForwardConn(ctx, conn, fwd)
	}
}

func (c *Client) handleForwardConn(ctx context.Context, conn net.Conn, fwd config.ForwardRule) {
	defer conn.Close()

	sessionID, err := c.openTunnel(fwd.RemoteClient, fwd.RemoteService, fwd.Protocol)
	if err != nil {
		c.logger.Error("open tunnel failed", zap.Error(err))
		return
	}
	if sessionID == 0 {
		c.logger.Error("tunnel allocation failed")
		return
	}

	c.mu.Lock()
	c.tunnels[sessionID] = &localTunnel{
		sessionID: sessionID,
		localConn: conn,
		protocol:  fwd.Protocol,
	}
	c.mu.Unlock()

	c.logger.Info("forward tunnel established",
		zap.Uint32("session", sessionID),
		zap.String("local", conn.RemoteAddr().String()),
		zap.String("remote", fmt.Sprintf("%s/%s", fwd.RemoteClient, fwd.RemoteService)))

	// Forward local -> server
	c.forwardLocalToServer(sessionID, conn)
}

func (c *Client) openTunnel(targetClient, targetService, protocol string) (uint32, error) {
	pendingKey := targetClient + "/" + targetService
	ch := make(chan uint32, 1)

	c.mu.Lock()
	c.pending[pendingKey] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, pendingKey)
		c.mu.Unlock()
	}()

	payload, err := proto.EncodeJSON(&proto.OpenTunnelPayload{
		TargetClient:  targetClient,
		TargetService: targetService,
		Protocol:      protocol,
	})
	if err != nil {
		return 0, err
	}

	if err := c.writer.WriteMessage(proto.NewMessage(proto.MsgOpenTunnel, 0, payload)); err != nil {
		return 0, fmt.Errorf("send open tunnel: %w", err)
	}

	// Wait up to 30 seconds for tunnel to be established
	timer := time.NewTimer(30 * time.Second)
	defer timer.Stop()

	select {
	case sessionID := <-ch:
		return sessionID, nil
	case <-timer.C:
		return 0, fmt.Errorf("tunnel open timeout")
	}
}
