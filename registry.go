package mcpmesh

import (
	"fmt"
	"sync"
	"time"
)

// Registry manages agent registration and discovery
type Registry struct {
	agents map[string]AgentInfo
	mu     sync.RWMutex
}

// NewRegistry creates a new in-memory agent registry
func NewRegistry() *Registry {
	return &Registry{
		agents: make(map[string]AgentInfo),
	}
}

// Register adds an agent to the registry
func (r *Registry) Register(info AgentInfo) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if info.ID == "" {
		return fmt.Errorf("agent ID required")
	}
	r.agents[info.ID] = info
	return nil
}

// Deregister removes an agent from the registry
func (r *Registry) Deregister(agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, ok := r.agents[agentID]; !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}
	delete(r.agents, agentID)
	return nil
}

// FindByCapability returns agents matching a capability name
func (r *Registry) FindByCapability(capability string) []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	var result []AgentInfo
	for _, agent := range r.agents {
		if agent.Status == StatusUnhealthy {
			continue
		}
		for _, cap := range agent.Capabilities {
			if cap.Name == capability {
				result = append(result, agent)
				break
			}
		}
	}
	return result
}

// Get retrieves a specific agent by ID
func (r *Registry) Get(agentID string) (AgentInfo, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.agents[agentID]
	return agent, ok
}

// ListAll returns all registered agents
func (r *Registry) ListAll() []AgentInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()

	result := make([]AgentInfo, 0, len(r.agents))
	for _, agent := range r.agents {
		result = append(result, agent)
	}
	return result
}

// UpdateHeartbeat updates the last heartbeat time for an agent
func (r *Registry) UpdateHeartbeat(agentID string) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	agent, ok := r.agents[agentID]
	if !ok {
		return fmt.Errorf("agent %s not found", agentID)
	}
	agent.LastHeartbeat = time.Now()
	r.agents[agentID] = agent
	return nil
}

// Count returns the number of registered agents
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.agents)
}
