// Package mcpmesh implements the MCP Agent Orchestration Mesh.
//
// MCP-Mesh provides a gRPC-based mesh network for the Elite Agent Collective,
// supporting agent discovery, capability-based routing, load balancing,
// and fault-tolerant execution across distributed agents.
//
// Architecture:
//   - Registry: Agent registration and capability tracking
//   - Router: Intelligent request routing with affinity
//   - Executor: Distributed agent execution with timeout/retry
//   - Monitor: Health checks and circuit breaker patterns
package mcpmesh

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
)

// AgentCapability represents a single capability an agent provides
type AgentCapability struct {
	Name        string            `json:"name" yaml:"name"`
	Version     string            `json:"version" yaml:"version"`
	Description string            `json:"description" yaml:"description"`
	Tags        []string          `json:"tags" yaml:"tags"`
	Metadata    map[string]string `json:"metadata" yaml:"metadata"`
}

// AgentInfo represents a registered agent in the mesh
type AgentInfo struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Tier         int               `json:"tier"`
	Capabilities []AgentCapability `json:"capabilities"`
	Endpoint     string            `json:"endpoint"`
	Status       AgentStatus       `json:"status"`
	RegisteredAt time.Time         `json:"registered_at"`
	LastHeartbeat time.Time        `json:"last_heartbeat"`
	Metadata     map[string]string `json:"metadata"`
}

// AgentStatus represents the health status of an agent
type AgentStatus int

const (
	StatusUnknown AgentStatus = iota
	StatusHealthy
	StatusDegraded
	StatusUnhealthy
	StatusDraining
)

func (s AgentStatus) String() string {
	switch s {
	case StatusHealthy:
		return "healthy"
	case StatusDegraded:
		return "degraded"
	case StatusUnhealthy:
		return "unhealthy"
	case StatusDraining:
		return "draining"
	default:
		return "unknown"
	}
}

// ExecutionRequest represents a request to execute an agent capability
type ExecutionRequest struct {
	RequestID    string            `json:"request_id"`
	Capability   string            `json:"capability"`
	Input        []byte            `json:"input"`
	Timeout      time.Duration     `json:"timeout"`
	Priority     int               `json:"priority"`
	Metadata     map[string]string `json:"metadata"`
	PreferAgent  string            `json:"prefer_agent,omitempty"`
}

// ExecutionResult represents the result of an agent execution
type ExecutionResult struct {
	RequestID  string        `json:"request_id"`
	AgentID    string        `json:"agent_id"`
	Output     []byte        `json:"output"`
	Duration   time.Duration `json:"duration"`
	Success    bool          `json:"success"`
	Error      string        `json:"error,omitempty"`
}

// MeshConfig holds configuration for the MCP mesh
type MeshConfig struct {
	ListenAddr       string        `yaml:"listen_addr"`
	RegistryBackend  string        `yaml:"registry_backend"` // "memory", "consul", "etcd"
	HeartbeatInterval time.Duration `yaml:"heartbeat_interval"`
	HealthTimeout    time.Duration `yaml:"health_timeout"`
	MaxRetries       int           `yaml:"max_retries"`
	CircuitBreaker   CircuitBreakerConfig `yaml:"circuit_breaker"`
	RyzansteinURL    string        `yaml:"ryzanstein_url"`
}

// CircuitBreakerConfig configures the circuit breaker
type CircuitBreakerConfig struct {
	Threshold   int           `yaml:"threshold"`
	Timeout     time.Duration `yaml:"timeout"`
	HalfOpenMax int           `yaml:"half_open_max"`
}

// DefaultConfig returns sensible default configuration
func DefaultConfig() MeshConfig {
	return MeshConfig{
		ListenAddr:        ":50051",
		RegistryBackend:   "memory",
		HeartbeatInterval: 10 * time.Second,
		HealthTimeout:     30 * time.Second,
		MaxRetries:        3,
		CircuitBreaker: CircuitBreakerConfig{
			Threshold:   5,
			Timeout:     60 * time.Second,
			HalfOpenMax: 2,
		},
		RyzansteinURL: "http://localhost:8000",
	}
}

// Mesh is the main orchestration mesh
type Mesh struct {
	config   MeshConfig
	registry *Registry
	router   *Router
	mu       sync.RWMutex
}

// NewMesh creates a new MCP mesh instance
func NewMesh(config MeshConfig) *Mesh {
	registry := NewRegistry()
	router := NewRouter(registry)
	return &Mesh{
		config:   config,
		registry: registry,
		router:   router,
	}
}

// RegisterAgent registers a new agent with the mesh
func (m *Mesh) RegisterAgent(ctx context.Context, info AgentInfo) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if info.ID == "" {
		info.ID = uuid.New().String()
	}
	info.RegisteredAt = time.Now()
	info.LastHeartbeat = time.Now()
	info.Status = StatusHealthy

	if err := m.registry.Register(info); err != nil {
		return "", fmt.Errorf("register agent: %w", err)
	}

	return info.ID, nil
}

// DiscoverAgents finds agents matching a capability query
func (m *Mesh) DiscoverAgents(ctx context.Context, capability string) ([]AgentInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.registry.FindByCapability(capability), nil
}

// Execute runs a capability on the best available agent via HTTP POST.
// Implements retry with circuit breaker logic.
func (m *Mesh) Execute(ctx context.Context, req ExecutionRequest) (*ExecutionResult, error) {
	if req.RequestID == "" {
		req.RequestID = uuid.New().String()
	}

	timeout := req.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	maxRetries := m.config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 1
	}

	var lastErr error
	for attempt := 0; attempt < maxRetries; attempt++ {
		agent, err := m.router.Route(req)
		if err != nil {
			return nil, fmt.Errorf("route request: %w", err)
		}

		// Check circuit breaker
		if m.router.IsCircuitOpen(agent.ID) {
			lastErr = fmt.Errorf("circuit breaker open for agent %s", agent.ID)
			continue
		}

		start := time.Now()

		reqCtx, cancel := context.WithTimeout(ctx, timeout)
		httpReq, err := http.NewRequestWithContext(reqCtx, http.MethodPost, agent.Endpoint, bytes.NewReader(req.Input))
		if err != nil {
			cancel()
			m.router.RecordFailure(agent.ID)
			lastErr = fmt.Errorf("create request: %w", err)
			continue
		}
		httpReq.Header.Set("Content-Type", "application/json")
		httpReq.Header.Set("X-Request-ID", req.RequestID)
		httpReq.Header.Set("X-Capability", req.Capability)

		client := &http.Client{Timeout: timeout}
		resp, err := client.Do(httpReq)
		cancel()

		if err != nil {
			m.router.RecordFailure(agent.ID)
			lastErr = fmt.Errorf("execute on %s: %w", agent.Name, err)
			continue
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()

		if resp.StatusCode >= 500 {
			m.router.RecordFailure(agent.ID)
			lastErr = fmt.Errorf("agent %s returned %d: %s", agent.Name, resp.StatusCode, string(body))
			continue
		}

		if resp.StatusCode >= 400 {
			m.router.RecordSuccess(agent.ID)
			return &ExecutionResult{
				RequestID: req.RequestID,
				AgentID:   agent.ID,
				Output:    body,
				Duration:  time.Since(start),
				Success:   false,
				Error:     fmt.Sprintf("agent returned %d", resp.StatusCode),
			}, nil
		}

		m.router.RecordSuccess(agent.ID)
		return &ExecutionResult{
			RequestID: req.RequestID,
			AgentID:   agent.ID,
			Output:    body,
			Duration:  time.Since(start),
			Success:   true,
		}, nil
	}

	return nil, fmt.Errorf("all %d attempts failed: %w", maxRetries, lastErr)
}

// Heartbeat updates an agent's last-seen timestamp
func (m *Mesh) Heartbeat(ctx context.Context, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registry.UpdateHeartbeat(agentID)
}

// DeregisterAgent removes an agent from the mesh
func (m *Mesh) DeregisterAgent(ctx context.Context, agentID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.registry.Deregister(agentID)
}

// Stats returns mesh statistics
func (m *Mesh) Stats() MeshStats {
	m.mu.RLock()
	defer m.mu.RUnlock()
	agents := m.registry.ListAll()
	healthy := 0
	for _, a := range agents {
		if a.Status == StatusHealthy {
			healthy++
		}
	}
	return MeshStats{
		TotalAgents:   len(agents),
		HealthyAgents: healthy,
	}
}

// MeshStats contains mesh statistics
type MeshStats struct {
	TotalAgents   int `json:"total_agents"`
	HealthyAgents int `json:"healthy_agents"`
}
