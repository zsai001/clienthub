package server

import (
	"github.com/cltx/clienthub/pkg/proto"
	"go.uber.org/zap"
)

type Relay struct {
	registry *Registry
	logger   *zap.Logger
}

func NewRelay(registry *Registry, logger *zap.Logger) *Relay {
	return &Relay{registry: registry, logger: logger}
}

// ForwardData routes a DATA or DATA_UDP message to the peer client in the tunnel.
func (r *Relay) ForwardData(senderName string, msg *proto.Message) {
	t := r.registry.GetTunnel(msg.SessionID)
	if t == nil {
		r.logger.Warn("tunnel not found for relay", zap.Uint32("session", msg.SessionID))
		return
	}

	var targetName string
	if senderName == t.SourceClient {
		targetName = t.TargetClient
	} else if senderName == t.TargetClient {
		targetName = t.SourceClient
	} else {
		r.logger.Warn("sender not part of tunnel",
			zap.String("sender", senderName),
			zap.Uint32("session", msg.SessionID))
		return
	}

	target := r.registry.GetClient(targetName)
	if target == nil {
		r.logger.Warn("target client not found", zap.String("target", targetName))
		return
	}

	if err := target.Writer.WriteMessage(msg); err != nil {
		r.logger.Error("relay write failed",
			zap.String("target", targetName),
			zap.Error(err))
	}
}

// ForwardClose sends a CLOSE message to the peer client.
func (r *Relay) ForwardClose(senderName string, msg *proto.Message) {
	t := r.registry.GetTunnel(msg.SessionID)
	if t == nil {
		return
	}

	var targetName string
	if senderName == t.SourceClient {
		targetName = t.TargetClient
	} else {
		targetName = t.SourceClient
	}

	target := r.registry.GetClient(targetName)
	if target != nil {
		_ = target.Writer.WriteMessage(msg)
	}

	r.registry.RemoveTunnel(msg.SessionID)
}
