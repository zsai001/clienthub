package server

import (
	"encoding/json"
	"os"
	"sync"

	"github.com/cltx/clienthub/pkg/proto"
)

// ClientRules holds the server-side stored expose and forward rules for one client.
type ClientRules struct {
	Expose  []proto.ExposeRule  `json:"expose"`
	Forward []proto.ForwardInfo `json:"forward"`
}

// Store persists client rules to a JSON file.
type Store struct {
	mu      sync.RWMutex
	path    string
	Clients map[string]*ClientRules `json:"clients"`
}

func NewStore(path string) (*Store, error) {
	s := &Store{
		path:    path,
		Clients: make(map[string]*ClientRules),
	}
	if err := s.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}
	return s, nil
}

func (s *Store) load() error {
	data, err := os.ReadFile(s.path)
	if err != nil {
		return err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return json.Unmarshal(data, s)
}

func (s *Store) save() error {
	s.mu.RLock()
	data, err := json.MarshalIndent(s, "", "  ")
	s.mu.RUnlock()
	if err != nil {
		return err
	}
	return os.WriteFile(s.path, data, 0644)
}

func (s *Store) GetClientRules(clientName string) *ClientRules {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.Clients[clientName]
}

// AddExpose adds or updates an expose rule for a client and persists.
func (s *Store) AddExpose(clientName string, rule proto.ExposeRule) error {
	s.mu.Lock()
	rules := s.getOrCreate(clientName)
	// Replace if same name exists.
	for i, r := range rules.Expose {
		if r.Name == rule.Name {
			rules.Expose[i] = rule
			s.mu.Unlock()
			return s.save()
		}
	}
	rules.Expose = append(rules.Expose, rule)
	s.mu.Unlock()
	return s.save()
}

// RemoveExpose removes an expose rule by service name and persists.
func (s *Store) RemoveExpose(clientName, serviceName string) error {
	s.mu.Lock()
	rules, ok := s.Clients[clientName]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	filtered := rules.Expose[:0]
	for _, r := range rules.Expose {
		if r.Name != serviceName {
			filtered = append(filtered, r)
		}
	}
	rules.Expose = filtered
	s.mu.Unlock()
	return s.save()
}

// AddForward adds or updates a forward rule for a client and persists.
func (s *Store) AddForward(clientName string, fwd proto.ForwardInfo) error {
	s.mu.Lock()
	rules := s.getOrCreate(clientName)
	for i, f := range rules.Forward {
		if f.ListenAddr == fwd.ListenAddr {
			rules.Forward[i] = fwd
			s.mu.Unlock()
			return s.save()
		}
	}
	rules.Forward = append(rules.Forward, fwd)
	s.mu.Unlock()
	return s.save()
}

// RemoveForward removes a forward rule by listen address and persists.
func (s *Store) RemoveForward(clientName, listenAddr string) error {
	s.mu.Lock()
	rules, ok := s.Clients[clientName]
	if !ok {
		s.mu.Unlock()
		return nil
	}
	filtered := rules.Forward[:0]
	for _, f := range rules.Forward {
		if f.ListenAddr != listenAddr {
			filtered = append(filtered, f)
		}
	}
	rules.Forward = filtered
	s.mu.Unlock()
	return s.save()
}

// ListExpose returns all stored expose rules, optionally filtered by client.
func (s *Store) ListExpose(clientName string) []proto.ExposeListEntry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []proto.ExposeListEntry
	for name, rules := range s.Clients {
		if clientName != "" && name != clientName {
			continue
		}
		for _, r := range rules.Expose {
			result = append(result, proto.ExposeListEntry{ClientName: name, Rule: r})
		}
	}
	return result
}

// getOrCreate must be called with s.mu held (write).
func (s *Store) getOrCreate(clientName string) *ClientRules {
	if s.Clients[clientName] == nil {
		s.Clients[clientName] = &ClientRules{}
	}
	return s.Clients[clientName]
}
