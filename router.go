package mcpmesh

import (
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"time"
)

// circuitState tracks circuit breaker state for a single agent
type circuitState struct {
	failures    int
	state       string // "closed", "open", "half-open"
	lastFailure time.Time
	threshold   int
	timeout     time.Duration
}

func (cs *circuitState) isOpen() bool {
	if cs.state == "open" {
		// Check if timeout has elapsed → transition to half-open
		if time.Since(cs.lastFailure) > cs.timeout {
			cs.state = "half-open"
			return false
		}
		return true
	}
	return false
}

func (cs *circuitState) recordFailure() {
	cs.failures++
	cs.lastFailure = time.Now()
	if cs.failures >= cs.threshold {
		cs.state = "open"
	}
}

func (cs *circuitState) recordSuccess() {
	cs.failures = 0
	cs.state = "closed"
}

// Router handles capability-based request routing to agents
type Router struct {
	registry *Registry
	breakers map[string]*circuitState
	mu       sync.Mutex
}

// NewRouter creates a Router backed by the given registry
func NewRouter(registry *Registry) *Router {
	return &Router{
		registry: registry,
		breakers: make(map[string]*circuitState),
	}
}

// IsCircuitOpen checks if the circuit breaker for an agent is open
func (r *Router) IsCircuitOpen(agentID string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, exists := r.breakers[agentID]
	if !exists {
		return false
	}
	return cs.isOpen()
}

// RecordFailure records a failure for an agent's circuit breaker
func (r *Router) RecordFailure(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, exists := r.breakers[agentID]
	if !exists {
		cs = &circuitState{threshold: 5, timeout: 60 * time.Second, state: "closed"}
		r.breakers[agentID] = cs
	}
	cs.recordFailure()
}

// RecordSuccess records a success for an agent's circuit breaker
func (r *Router) RecordSuccess(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cs, exists := r.breakers[agentID]
	if !exists {
		return
	}
	cs.recordSuccess()
}

// RoutingStrategy defines how to select an agent
type RoutingStrategy int

const (
	StrategyRoundRobin RoutingStrategy = iota
	StrategyRandom
	StrategyLeastLoaded
	StrategyAffinity
)

// Route selects the best agent for the given request
func (r *Router) Route(req ExecutionRequest) (*AgentInfo, error) {
	candidates := r.registry.FindByCapability(req.Capability)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no agents found for capability: %s", req.Capability)
	}

	// Prefer specified agent
	if req.PreferAgent != "" {
		for i, c := range candidates {
			if c.ID == req.PreferAgent && c.Status == StatusHealthy {
				return &candidates[i], nil
			}
		}
	}

	// Filter to healthy agents only, skip those with open circuit breakers
	healthy := make([]AgentInfo, 0)
	for _, c := range candidates {
		if (c.Status == StatusHealthy || c.Status == StatusDegraded) && !r.IsCircuitOpen(c.ID) {
			healthy = append(healthy, c)
		}
	}
	if len(healthy) == 0 {
		return nil, fmt.Errorf("no healthy agents for capability: %s", req.Capability)
	}

	// Sort by tier (lower tier = higher priority)
	sort.Slice(healthy, func(i, j int) bool {
		return healthy[i].Tier < healthy[j].Tier
	})

	// Among same-tier agents, pick randomly
	topTier := healthy[0].Tier
	sameTier := make([]AgentInfo, 0)
	for _, h := range healthy {
		if h.Tier == topTier {
			sameTier = append(sameTier, h)
		}
	}

	idx := rand.Intn(len(sameTier))
	return &sameTier[idx], nil
}

// RouteAll returns all agents capable of handling the request
func (r *Router) RouteAll(req ExecutionRequest) ([]AgentInfo, error) {
	candidates := r.registry.FindByCapability(req.Capability)
	if len(candidates) == 0 {
		return nil, fmt.Errorf("no agents found for capability: %s", req.Capability)
	}
	return candidates, nil
}
