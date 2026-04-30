package mcpmesh

import (
	"context"
	"fmt"
	"sync"
	"time"

	"go.uber.org/zap"
)

// HeartbeatMonitor watches registered agents and marks them Degraded or
// Unhealthy when they stop sending heartbeats within the configured window.
//
// Design:
//   - Each agent is expected to call mesh.Heartbeat() at least once per
//     HealthyWindow interval.
//   - If an agent misses one window it becomes Degraded.
//   - If it misses two consecutive windows it becomes Unhealthy.
//   - Unhealthy agents are excluded from discovery (handled in Registry).
//
// Usage:
//
//	monitor := NewHeartbeatMonitor(mesh, logger, HeartbeatConfig{})
//	go monitor.Run(ctx)
type HeartbeatMonitor struct {
	mesh    *Mesh
	logger  *zap.Logger
	config  HeartbeatConfig
	tickers map[string]*time.Ticker
	mu      sync.Mutex
}

// HeartbeatConfig controls timing thresholds for the heartbeat monitor.
type HeartbeatConfig struct {
	// CheckInterval is how often the monitor sweeps all agents (default: 15s).
	CheckInterval time.Duration

	// HealthyWindow is the time an agent can be silent before becoming Degraded
	// (default: 30s).
	HealthyWindow time.Duration

	// UnhealthyWindow is the time an agent can be silent before becoming
	// Unhealthy (default: 60s).
	UnhealthyWindow time.Duration
}

// DefaultHeartbeatConfig returns sensible production defaults.
func DefaultHeartbeatConfig() HeartbeatConfig {
	return HeartbeatConfig{
		CheckInterval:   15 * time.Second,
		HealthyWindow:   30 * time.Second,
		UnhealthyWindow: 60 * time.Second,
	}
}

// NewHeartbeatMonitor creates a HeartbeatMonitor for the given Mesh.
func NewHeartbeatMonitor(mesh *Mesh, logger *zap.Logger, cfg HeartbeatConfig) *HeartbeatMonitor {
	if cfg.CheckInterval == 0 {
		cfg = DefaultHeartbeatConfig()
	}
	return &HeartbeatMonitor{
		mesh:   mesh,
		logger: logger,
		config: cfg,
	}
}

// Run starts the sweep loop and blocks until ctx is cancelled.
func (m *HeartbeatMonitor) Run(ctx context.Context) {
	ticker := time.NewTicker(m.config.CheckInterval)
	defer ticker.Stop()

	m.logger.Info("heartbeat monitor started",
		zap.Duration("check_interval", m.config.CheckInterval),
		zap.Duration("healthy_window", m.config.HealthyWindow),
		zap.Duration("unhealthy_window", m.config.UnhealthyWindow),
	)

	for {
		select {
		case <-ctx.Done():
			m.logger.Info("heartbeat monitor stopped")
			return
		case <-ticker.C:
			m.sweep()
		}
	}
}

// sweep iterates all registered agents and updates their status based on their
// LastHeartbeat timestamp.
func (m *HeartbeatMonitor) sweep() {
	agents := m.mesh.registry.ListAll()
	now := time.Now()

	degraded, unhealthy := 0, 0
	for _, agent := range agents {
		age := now.Sub(agent.LastHeartbeat)

		var newStatus AgentStatus
		switch {
		case age > m.config.UnhealthyWindow:
			newStatus = StatusUnhealthy
			unhealthy++
		case age > m.config.HealthyWindow:
			newStatus = StatusDegraded
			degraded++
		default:
			// Still within healthy window — leave status as-is
			continue
		}

		if agent.Status == newStatus {
			continue // no change needed
		}

		agent.Status = newStatus
		if err := m.mesh.registry.Register(agent); err != nil {
			m.logger.Warn("heartbeat sweep: failed to update agent status",
				zap.String("agent_id", agent.ID),
				zap.Error(err),
			)
			continue
		}

		m.logger.Warn("agent health degraded",
			zap.String("agent_id", agent.ID),
			zap.String("name", agent.Name),
			zap.String("status", fmt.Sprint(newStatus)),
			zap.Duration("silence", age.Round(time.Second)),
		)
	}

	if degraded+unhealthy > 0 {
		m.logger.Info("heartbeat sweep complete",
			zap.Int("total", len(agents)),
			zap.Int("degraded", degraded),
			zap.Int("unhealthy", unhealthy),
		)
	}
}
