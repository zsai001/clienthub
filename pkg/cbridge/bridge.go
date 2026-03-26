package cbridge

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/cltx/clienthub/config"
	"github.com/cltx/clienthub/pkg/client"
	"github.com/cltx/clienthub/pkg/manager"
	"github.com/cltx/clienthub/pkg/proto"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Hub is the top-level object that native UIs interact with.
type Hub struct {
	Registry  *HandleRegistry
	Callbacks *Callbacks
}

func NewHub() *Hub {
	return &Hub{
		Registry:  NewHandleRegistry(),
		Callbacks: NewCallbacks(),
	}
}

// ClientHandle bundles a Client with its cancel function and state.
type ClientHandle struct {
	Client *client.Client
	Cancel context.CancelFunc
	Cfg    *config.ClientConfig
	Mu     sync.Mutex
	Status int
}

// --- Client operations ---

func (h *Hub) ClientCreate(configJSON string) int {
	var cfg config.ClientConfig
	if err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		h.Callbacks.FireLog(2, "ClientCreate: invalid config JSON: "+err.Error())
		return -1
	}
	if cfg.ServerAddr == "" || cfg.ClientName == "" || cfg.Secret == "" {
		h.Callbacks.FireLog(2, "ClientCreate: server_addr, client_name, and secret are required")
		return -1
	}
	for i := range cfg.Expose {
		if cfg.Expose[i].Protocol == "" {
			cfg.Expose[i].Protocol = "tcp"
		}
	}
	for i := range cfg.Forward {
		if cfg.Forward[i].Protocol == "" {
			cfg.Forward[i].Protocol = "tcp"
		}
	}

	logger := h.newCallbackLogger()
	c := client.New(&cfg, logger)

	ch := &ClientHandle{
		Client: c,
		Cfg:    &cfg,
		Status: StatusDisconnected,
	}
	id := h.Registry.Put(ch)
	return id
}

func (h *Hub) ClientStart(handle int) {
	obj, ok := h.Registry.Get(handle)
	if !ok {
		return
	}
	ch := obj.(*ClientHandle)

	ch.Mu.Lock()
	if ch.Cancel != nil {
		ch.Mu.Unlock()
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	ch.Cancel = cancel
	ch.Status = StatusConnecting
	ch.Mu.Unlock()

	h.Callbacks.FireStatus(handle, StatusConnecting, "connecting")

	go func() {
		err := ch.Client.Run(ctx)

		ch.Mu.Lock()
		ch.Status = StatusDisconnected
		ch.Cancel = nil
		ch.Mu.Unlock()

		detail := "stopped"
		if err != nil {
			detail = err.Error()
		}
		h.Callbacks.FireStatus(handle, StatusDisconnected, detail)
	}()
}

func (h *Hub) ClientStop(handle int) {
	obj, ok := h.Registry.Get(handle)
	if !ok {
		return
	}
	ch := obj.(*ClientHandle)

	ch.Mu.Lock()
	if ch.Cancel != nil {
		ch.Cancel()
		ch.Cancel = nil
	}
	ch.Mu.Unlock()
}

func (h *Hub) ClientDestroy(handle int) {
	h.ClientStop(handle)
	h.Registry.Delete(handle)
}

func (h *Hub) ClientGetStatus(handle int) string {
	obj, ok := h.Registry.Get(handle)
	if !ok {
		return `{"error":"invalid handle"}`
	}
	ch := obj.(*ClientHandle)
	ch.Mu.Lock()
	status := ch.Status
	ch.Mu.Unlock()

	result := map[string]any{
		"status":      status,
		"client_name": ch.Cfg.ClientName,
		"server_addr": ch.Cfg.ServerAddr,
	}
	data, _ := json.Marshal(result)
	return string(data)
}

// --- Manager operations ---

func (h *Hub) ManagerCreate(addr, secret string) int {
	if addr == "" || secret == "" {
		h.Callbacks.FireLog(2, "ManagerCreate: addr and secret are required")
		return -1
	}
	logger := h.newCallbackLogger()
	m := manager.New(addr, secret, logger)
	id := h.Registry.Put(m)
	return id
}

func (h *Hub) getManager(handle int) (*manager.Manager, bool) {
	obj, ok := h.Registry.Get(handle)
	if !ok {
		return nil, false
	}
	m, ok := obj.(*manager.Manager)
	return m, ok
}

func (h *Hub) ManagerListClients(handle int) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	resp, err := m.SendCommand(proto.MsgListClients, nil)
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerListTunnels(handle int) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	resp, err := m.SendCommand(proto.MsgListTunnels, nil)
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerListForwards(handle int) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	resp, err := m.SendCommand(proto.MsgListForwards, nil)
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerAddForward(handle int, paramsJSON string) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	resp, err := m.SendCommand(proto.MsgAddForward, []byte(paramsJSON))
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerRemoveForward(handle int, paramsJSON string) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	resp, err := m.SendCommand(proto.MsgRemoveForward, []byte(paramsJSON))
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerKickClient(handle int, name string) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	payload, _ := json.Marshal(map[string]string{"client_name": name})
	resp, err := m.SendCommand(proto.MsgKickClient, payload)
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerStatus(handle int) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	clientResp, err := m.SendCommand(proto.MsgListClients, nil)
	if err != nil {
		return errResult(err.Error())
	}
	tunnelResp, err := m.SendCommand(proto.MsgListTunnels, nil)
	if err != nil {
		return errResult(err.Error())
	}

	var clients []json.RawMessage
	var tunnels []json.RawMessage
	if clientResp.Data != nil {
		_ = json.Unmarshal(clientResp.Data, &clients)
	}
	if tunnelResp.Data != nil {
		_ = json.Unmarshal(tunnelResp.Data, &tunnels)
	}

	result := map[string]any{
		"success":      true,
		"reachable":    true,
		"client_count": len(clients),
		"tunnel_count": len(tunnels),
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func (h *Hub) ManagerListExpose(handle int, clientName string) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	var payload []byte
	if clientName != "" {
		payload, _ = json.Marshal(map[string]string{"client_name": clientName})
	}
	resp, err := m.SendCommand(proto.MsgListExpose, payload)
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerAddExpose(handle int, paramsJSON string) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	resp, err := m.SendCommand(proto.MsgAddExpose, []byte(paramsJSON))
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerRemoveExpose(handle int, paramsJSON string) string {
	m, ok := h.getManager(handle)
	if !ok {
		return errResult("invalid handle")
	}
	resp, err := m.SendCommand(proto.MsgRemoveExpose, []byte(paramsJSON))
	if err != nil {
		return errResult(err.Error())
	}
	return respResult(resp)
}

func (h *Hub) ManagerDestroy(handle int) {
	h.Registry.Delete(handle)
}

// --- Helpers ---

func errResult(msg string) string {
	result := map[string]any{
		"success": false,
		"message": msg,
	}
	data, _ := json.Marshal(result)
	return string(data)
}

func respResult(resp *proto.ResponsePayload) string {
	data, _ := json.Marshal(resp)
	return string(data)
}

func (h *Hub) newCallbackLogger() *zap.Logger {
	core := &callbackCore{cb: h.Callbacks}
	return zap.New(core)
}

type callbackCore struct {
	cb *Callbacks
}

func (c *callbackCore) Enabled(zapcore.Level) bool                          { return true }
func (c *callbackCore) With([]zapcore.Field) zapcore.Core                   { return c }
func (c *callbackCore) Check(ent zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	return ce.After(ent, c)
}
func (c *callbackCore) OnWrite(_ *zapcore.CheckedEntry, _ []zapcore.Field)  {}
func (c *callbackCore) Write(ent zapcore.Entry, fields []zapcore.Field) error {
	level := 0
	switch {
	case ent.Level >= zapcore.ErrorLevel:
		level = 3
	case ent.Level >= zapcore.WarnLevel:
		level = 2
	case ent.Level >= zapcore.InfoLevel:
		level = 1
	}
	msg := ent.Message
	if len(fields) > 0 {
		enc := zapcore.NewMapObjectEncoder()
		for _, f := range fields {
			f.AddTo(enc)
		}
		if extra, err := json.Marshal(enc.Fields); err == nil {
			msg += " " + string(extra)
		}
	}
	c.cb.FireLog(level, msg)
	return nil
}
func (c *callbackCore) Sync() error { return nil }
