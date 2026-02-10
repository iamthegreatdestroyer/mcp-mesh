# MCP-Mesh — Agent Orchestration Mesh

**Tier:** 1 (Ecosystem-locked)  
**Language:** Go  
**License:** AGPL-3.0

## Overview

MCP-Mesh provides a gRPC-based mesh network for the Elite Agent Collective,
supporting agent discovery, capability-based routing, load balancing,
and fault-tolerant execution across distributed agents.

## Features

- **Agent Registry**: Register agents with capabilities and metadata
- **Capability Routing**: Route requests to the best available agent by tier and health
- **Health Monitoring**: Heartbeat-based health tracking with circuit breaker
- **Agent Preference**: Optionally prefer specific agents for requests
- **Mesh Statistics**: Real-time agent count and health metrics

## Usage

```go
mesh := mcpmesh.NewMesh(mcpmesh.DefaultConfig())

// Register an agent
id, _ := mesh.RegisterAgent(ctx, mcpmesh.AgentInfo{
    Name: "cipher",
    Tier: 1,
    Capabilities: []mcpmesh.AgentCapability{
        {Name: "security_audit", Version: "1.0"},
    },
})

// Execute a capability
result, _ := mesh.Execute(ctx, mcpmesh.ExecutionRequest{
    Capability: "security_audit",
    Input:      []byte("review this code"),
})
```

## gRPC Protocol

Implements the `EliteAgentCollective` protobuf service:
- `RegisterAgent` — Register an agent
- `DiscoverAgents` — Find agents by capability
- `ExecuteAgent` — Execute on best agent
- `StreamExecution` — Streaming execution
