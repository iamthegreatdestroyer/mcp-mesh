package mcpmesh

import (
	"fmt"
	"math/rand"
	"sort"
)

// Router handles capability-based request routing to agents
type Router struct {
	registry *Registry
}

// NewRouter creates a Router backed by the given registry
func NewRouter(registry *Registry) *Router {
	return &Router{registry: registry}
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

	// Filter to healthy agents only
	healthy := make([]AgentInfo, 0)
	for _, c := range candidates {
		if c.Status == StatusHealthy || c.Status == StatusDegraded {
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
