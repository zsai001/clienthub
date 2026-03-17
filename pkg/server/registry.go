package server

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/cltx/clienthub/pkg/proto"
	"github.com/cltx/clienthub/pkg/tunnel"
)

type ClientSession struct {
	Name        string
	Conn        net.Conn
	Writer      *tunnel.ConnWriter
	Services    []proto.ServiceInfo
	ConnectedAt time.Time
	done        chan struct{}
}

type TunnelSession struct {
	ID            uint32
	SourceClient  string
	TargetClient  string
	TargetService string
	Protocol      string
	CreatedAt     time.Time
}

type Registry struct {
	mu       sync.RWMutex
	clients  map[string]*ClientSession
	tunnels  map[uint32]*TunnelSession
	nextSess uint32
}

func NewRegistry() *Registry {
	return &Registry{
		clients: make(map[string]*ClientSession),
		tunnels: make(map[uint32]*TunnelSession),
	}
}

func (r *Registry) AddClient(session *ClientSession) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clients[session.Name] = session
}

func (r *Registry) RemoveClient(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if sess, ok := r.clients[name]; ok {
		close(sess.done)
		sess.Conn.Close()
		delete(r.clients, name)
	}
	// Clean up tunnels involving this client
	for id, t := range r.tunnels {
		if t.SourceClient == name || t.TargetClient == name {
			delete(r.tunnels, id)
		}
	}
}

func (r *Registry) GetClient(name string) *ClientSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[name]
}

func (r *Registry) ListClients() []proto.ClientInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]proto.ClientInfo, 0, len(r.clients))
	for _, c := range r.clients {
		result = append(result, proto.ClientInfo{
			Name:        c.Name,
			Addr:        c.Conn.RemoteAddr().String(),
			Services:    c.Services,
			ConnectedAt: c.ConnectedAt.Format(time.RFC3339),
		})
	}
	return result
}

func (r *Registry) AllocateSession(source, target, service, protocol string) uint32 {
	id := atomic.AddUint32(&r.nextSess, 1)
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tunnels[id] = &TunnelSession{
		ID:            id,
		SourceClient:  source,
		TargetClient:  target,
		TargetService: service,
		Protocol:      protocol,
		CreatedAt:     time.Now(),
	}
	return id
}

func (r *Registry) GetTunnel(id uint32) *TunnelSession {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.tunnels[id]
}

func (r *Registry) RemoveTunnel(id uint32) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tunnels, id)
}

func (r *Registry) ListTunnels() []proto.TunnelInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	result := make([]proto.TunnelInfo, 0, len(r.tunnels))
	for _, t := range r.tunnels {
		result = append(result, proto.TunnelInfo{
			SessionID:     t.ID,
			SourceClient:  t.SourceClient,
			TargetClient:  t.TargetClient,
			TargetService: t.TargetService,
			Protocol:      t.Protocol,
		})
	}
	return result
}

func (r *Registry) UpdateServices(name string, services []proto.ServiceInfo) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.clients[name]; ok {
		c.Services = services
	}
}
