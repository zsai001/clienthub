package client

import (
	"context"
	"fmt"
	"net"
	"sync/atomic"
	"time"

	"github.com/cltx/clienthub/pkg/proto"
	"go.uber.org/zap"
)

func (c *Client) runForwardProxy(ctx context.Context, ln net.Listener, fwd *activeForward) {
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
				c.logger.Info("forward proxy stopped", zap.String("listen", fwd.ListenAddr))
				return
			default:
				c.logger.Debug("proxy accept error", zap.Error(err))
				continue
			}
		}
		go c.handleForwardConn(ctx, conn, fwd)
	}
}

func (c *Client) handleForwardConn(ctx context.Context, conn net.Conn, fwd *activeForward) {
	defer conn.Close()

	sessionID, err := c.openTunnel(ctx, fwd.RemoteClient, fwd.RemoteService, fwd.Protocol)
	if err != nil {
		c.logger.Error("open tunnel failed", zap.Error(err))
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

	c.forwardLocalToServer(sessionID, conn)
}

func (c *Client) openTunnel(ctx context.Context, targetClient, targetService, protocol string) (uint32, error) {
	// Allocate a unique request ID for this tunnel open request.
	reqID := atomic.AddUint32(&c.nextReqID, 1)
	ch := make(chan uint32, 1)

	c.mu.Lock()
	c.pending[reqID] = ch
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		delete(c.pending, reqID)
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

	// Use reqID as SessionID so the server echoes it back in TunnelReady/TunnelFail,
	// allowing us to match the response to this specific request.
	if err := c.writer.WriteMessage(proto.NewMessage(proto.MsgOpenTunnel, reqID, payload)); err != nil {
		return 0, fmt.Errorf("send open tunnel: %w", err)
	}

	timer := time.NewTimer(15 * time.Second)
	defer timer.Stop()

	select {
	case sessionID := <-ch:
		if sessionID == 0 {
			return 0, fmt.Errorf("tunnel open failed (target unavailable)")
		}
		return sessionID, nil
	case <-timer.C:
		return 0, fmt.Errorf("tunnel open timeout")
	case <-ctx.Done():
		return 0, ctx.Err()
	}
}
