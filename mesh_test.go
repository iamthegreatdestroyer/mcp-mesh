package mcpmesh

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func makeTestAgent(id, name string, tier int, caps ...string) AgentInfo {
	capabilities := make([]AgentCapability, len(caps))
	for i, c := range caps {
		capabilities[i] = AgentCapability{Name: c, Version: "1.0"}
	}
	return AgentInfo{
		ID:           id,
		Name:         name,
		Tier:         tier,
		Capabilities: capabilities,
		Endpoint:     "localhost:50052",
		Status:       StatusHealthy,
	}
}

func TestMeshRegisterAndDiscover(t *testing.T) {
	mesh := NewMesh(DefaultConfig())
	ctx := context.Background()

	agent := makeTestAgent("", "apex", 1, "code_review", "refactor")
	id, err := mesh.RegisterAgent(ctx, agent)
	require.NoError(t, err)
	assert.NotEmpty(t, id)

	agents, err := mesh.DiscoverAgents(ctx, "code_review")
	require.NoError(t, err)
	assert.Len(t, agents, 1)
	assert.Equal(t, "apex", agents[0].Name)
}

func TestMeshExecute(t *testing.T) {
	mesh := NewMesh(DefaultConfig())
	ctx := context.Background()

	_, err := mesh.RegisterAgent(ctx, makeTestAgent("agent-1", "cipher", 1, "security_audit"))
	require.NoError(t, err)

	result, err := mesh.Execute(ctx, ExecutionRequest{
		Capability: "security_audit",
		Input:      []byte("review this code"),
		Timeout:    5 * time.Second,
	})
	require.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, "agent-1", result.AgentID)
}

func TestMeshExecuteNoAgent(t *testing.T) {
	mesh := NewMesh(DefaultConfig())
	ctx := context.Background()

	_, err := mesh.Execute(ctx, ExecutionRequest{
		Capability: "nonexistent",
	})
	assert.Error(t, err)
}

func TestMeshHeartbeat(t *testing.T) {
	mesh := NewMesh(DefaultConfig())
	ctx := context.Background()

	_, err := mesh.RegisterAgent(ctx, makeTestAgent("hb-1", "flux", 2, "deploy"))
	require.NoError(t, err)

	err = mesh.Heartbeat(ctx, "hb-1")
	assert.NoError(t, err)

	err = mesh.Heartbeat(ctx, "nonexistent")
	assert.Error(t, err)
}

func TestMeshDeregister(t *testing.T) {
	mesh := NewMesh(DefaultConfig())
	ctx := context.Background()

	_, err := mesh.RegisterAgent(ctx, makeTestAgent("dr-1", "tensor", 2, "ml"))
	require.NoError(t, err)

	err = mesh.DeregisterAgent(ctx, "dr-1")
	assert.NoError(t, err)

	agents, _ := mesh.DiscoverAgents(ctx, "ml")
	assert.Empty(t, agents)
}

func TestMeshStats(t *testing.T) {
	mesh := NewMesh(DefaultConfig())
	ctx := context.Background()

	mesh.RegisterAgent(ctx, makeTestAgent("s1", "a1", 1, "cap1"))
	mesh.RegisterAgent(ctx, makeTestAgent("s2", "a2", 2, "cap2"))

	stats := mesh.Stats()
	assert.Equal(t, 2, stats.TotalAgents)
	assert.Equal(t, 2, stats.HealthyAgents)
}

func TestRegistryFindByCapability(t *testing.T) {
	r := NewRegistry()
	r.Register(makeTestAgent("1", "a", 1, "code", "review"))
	r.Register(makeTestAgent("2", "b", 2, "review"))
	r.Register(makeTestAgent("3", "c", 1, "deploy"))

	found := r.FindByCapability("review")
	assert.Len(t, found, 2)

	found = r.FindByCapability("deploy")
	assert.Len(t, found, 1)

	found = r.FindByCapability("nonexistent")
	assert.Empty(t, found)
}

func TestRegistryUnhealthyExcluded(t *testing.T) {
	r := NewRegistry()
	agent := makeTestAgent("u1", "unhealthy", 1, "cap")
	agent.Status = StatusUnhealthy
	r.Register(agent)

	found := r.FindByCapability("cap")
	assert.Empty(t, found)
}

func TestRouterTierPriority(t *testing.T) {
	r := NewRegistry()
	r.Register(makeTestAgent("t1", "tier1", 1, "task"))
	r.Register(makeTestAgent("t2", "tier2", 2, "task"))

	router := NewRouter(r)
	agent, err := router.Route(ExecutionRequest{Capability: "task"})
	require.NoError(t, err)
	assert.Equal(t, 1, agent.Tier)
}

func TestRouterPreferAgent(t *testing.T) {
	r := NewRegistry()
	r.Register(makeTestAgent("p1", "agent1", 1, "task"))
	r.Register(makeTestAgent("p2", "agent2", 1, "task"))

	router := NewRouter(r)
	agent, err := router.Route(ExecutionRequest{
		Capability:  "task",
		PreferAgent: "p2",
	})
	require.NoError(t, err)
	assert.Equal(t, "p2", agent.ID)
}

func TestAgentStatusString(t *testing.T) {
	assert.Equal(t, "healthy", StatusHealthy.String())
	assert.Equal(t, "unhealthy", StatusUnhealthy.String())
	assert.Equal(t, "unknown", StatusUnknown.String())
}

func TestDefaultConfig(t *testing.T) {
	config := DefaultConfig()
	assert.Equal(t, ":50051", config.ListenAddr)
	assert.Equal(t, "memory", config.RegistryBackend)
	assert.Equal(t, 3, config.MaxRetries)
}
