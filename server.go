package mcpmesh

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"time"

	"go.uber.org/zap"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/reflection"
	"google.golang.org/grpc/status"
)

// ─────────────────────────────────────────────────────────────────────────────
// Wire-format types (until protoc generates these from elite_agents.proto)
// These structs are JSON-marshallable and match the proto field names exactly.
// ─────────────────────────────────────────────────────────────────────────────

// GRPCRegisterRequest mirrors proto RegisterAgentRequest.
type GRPCRegisterRequest struct {
	Agent AgentInfo `json:"agent"`
}

// GRPCRegisterResponse mirrors proto RegisterAgentResponse.
type GRPCRegisterResponse struct {
	AgentID string `json:"agent_id"`
}

// GRPCDiscoverRequest mirrors proto DiscoverAgentsRequest.
type GRPCDiscoverRequest struct {
	Capability string `json:"capability"`
	MinTier    int    `json:"min_tier,omitempty"`
}

// GRPCDiscoverResponse mirrors proto DiscoverAgentsResponse.
type GRPCDiscoverResponse struct {
	Agents []AgentInfo `json:"agents"`
}

// GRPCExecuteRequest mirrors proto ExecuteAgentRequest.
type GRPCExecuteRequest struct {
	RequestID   string            `json:"request_id,omitempty"`
	Capability  string            `json:"capability"`
	Input       []byte            `json:"input,omitempty"`
	TimeoutMs   int64             `json:"timeout_ms,omitempty"`
	Priority    int               `json:"priority,omitempty"`
	PreferAgent string            `json:"prefer_agent,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// GRPCExecuteResponse mirrors proto ExecuteAgentResponse.
type GRPCExecuteResponse struct {
	RequestID  string `json:"request_id"`
	AgentID    string `json:"agent_id"`
	Output     []byte `json:"output,omitempty"`
	DurationMs int64  `json:"duration_ms"`
	Success    bool   `json:"success"`
	Error      string `json:"error,omitempty"`
}

// GRPCHeartbeatRequest mirrors proto HeartbeatRequest.
type GRPCHeartbeatRequest struct {
	AgentID   string `json:"agent_id"`
	Timestamp int64  `json:"timestamp,omitempty"`
}

// GRPCHeartbeatResponse mirrors proto HeartbeatResponse.
type GRPCHeartbeatResponse struct {
	Accepted   bool   `json:"accepted"`
	Message    string `json:"message,omitempty"`
	ServerTime int64  `json:"server_time"`
}

// ─────────────────────────────────────────────────────────────────────────────
// MeshGRPCServer
// ─────────────────────────────────────────────────────────────────────────────

// MeshGRPCServer wraps a Mesh and exposes it over gRPC using a raw JSON codec.
// Once proto codegen is wired up, replace the codec with the generated stubs.
//
// Usage:
//
//	srv := NewMeshGRPCServer(mesh, ":50051", logger)
//	srv.Serve(ctx)  // blocks until ctx is cancelled
type MeshGRPCServer struct {
	mesh   *Mesh
	addr   string
	logger *zap.Logger
	grpc   *grpc.Server
}

// NewMeshGRPCServer creates a gRPC server wrapping the given Mesh.
func NewMeshGRPCServer(mesh *Mesh, addr string, logger *zap.Logger) *MeshGRPCServer {
	gs := grpc.NewServer(
		grpc.UnaryInterceptor(loggingUnaryInterceptor(logger)),
	)

	srv := &MeshGRPCServer{
		mesh:   mesh,
		addr:   addr,
		logger: logger,
		grpc:   gs,
	}

	// Register reflection for grpcurl / grpc-gateway discovery
	reflection.Register(gs)

	// Register the JSON-over-gRPC service handler.
	// ServiceDesc declared at the bottom of this file.
	gs.RegisterService(&_EliteAgentMesh_serviceDesc, srv)

	return srv
}

// Serve starts the gRPC listener and blocks until ctx is cancelled.
func (s *MeshGRPCServer) Serve(ctx context.Context) error {
	lis, err := net.Listen("tcp", s.addr)
	if err != nil {
		return fmt.Errorf("grpc listen %s: %w", s.addr, err)
	}

	s.logger.Info("gRPC server listening", zap.String("addr", s.addr))

	errCh := make(chan error, 1)
	go func() {
		errCh <- s.grpc.Serve(lis)
	}()

	select {
	case <-ctx.Done():
		s.logger.Info("gRPC server shutting down gracefully")
		s.grpc.GracefulStop()
		return nil
	case err := <-errCh:
		return fmt.Errorf("grpc serve: %w", err)
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// Service method implementations
// ─────────────────────────────────────────────────────────────────────────────

func (s *MeshGRPCServer) registerAgent(ctx context.Context, rawReq []byte) ([]byte, error) {
	var req GRPCRegisterRequest
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode RegisterAgent: %v", err)
	}
	agentID, err := s.mesh.RegisterAgent(ctx, req.Agent)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "RegisterAgent: %v", err)
	}
	return json.Marshal(GRPCRegisterResponse{AgentID: agentID})
}

func (s *MeshGRPCServer) discoverAgents(ctx context.Context, rawReq []byte) ([]byte, error) {
	var req GRPCDiscoverRequest
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode DiscoverAgents: %v", err)
	}
	agents, err := s.mesh.DiscoverAgents(ctx, req.Capability)
	if err != nil {
		return nil, status.Errorf(codes.Internal, "DiscoverAgents: %v", err)
	}
	return json.Marshal(GRPCDiscoverResponse{Agents: agents})
}

func (s *MeshGRPCServer) executeAgent(ctx context.Context, rawReq []byte) ([]byte, error) {
	var req GRPCExecuteRequest
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode ExecuteAgent: %v", err)
	}

	timeout := time.Duration(req.TimeoutMs) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	result, err := s.mesh.Execute(ctx, ExecutionRequest{
		RequestID:  req.RequestID,
		Capability: req.Capability,
		Input:      req.Input,
		Timeout:    timeout,
	})
	if err != nil {
		return nil, status.Errorf(codes.Unavailable, "Execute: %v", err)
	}

	return json.Marshal(GRPCExecuteResponse{
		RequestID:  result.RequestID,
		AgentID:    result.AgentID,
		Output:     result.Output,
		DurationMs: result.Duration.Milliseconds(),
		Success:    result.Success,
	})
}

func (s *MeshGRPCServer) heartbeat(ctx context.Context, rawReq []byte) ([]byte, error) {
	var req GRPCHeartbeatRequest
	if err := json.Unmarshal(rawReq, &req); err != nil {
		return nil, status.Errorf(codes.InvalidArgument, "decode Heartbeat: %v", err)
	}
	if err := s.mesh.Heartbeat(ctx, req.AgentID); err != nil {
		return nil, status.Errorf(codes.NotFound, "Heartbeat: %v", err)
	}
	return json.Marshal(GRPCHeartbeatResponse{
		Accepted:   true,
		ServerTime: time.Now().UnixMilli(),
	})
}

func (s *MeshGRPCServer) listAgents(ctx context.Context, rawReq []byte) ([]byte, error) {
	agents := s.mesh.registry.ListAll()
	type resp struct {
		Agents []AgentInfo `json:"agents"`
		Total  int         `json:"total"`
	}
	return json.Marshal(resp{Agents: agents, Total: len(agents)})
}

// ─────────────────────────────────────────────────────────────────────────────
// gRPC logging interceptor
// ─────────────────────────────────────────────────────────────────────────────

func loggingUnaryInterceptor(log *zap.Logger) grpc.UnaryServerInterceptor {
	return func(
		ctx context.Context,
		req interface{},
		info *grpc.UnaryServerInfo,
		handler grpc.UnaryHandler,
	) (interface{}, error) {
		start := time.Now()
		resp, err := handler(ctx, req)
		log.Info("grpc",
			zap.String("method", info.FullMethod),
			zap.Duration("duration", time.Since(start)),
			zap.Bool("ok", err == nil),
		)
		return resp, err
	}
}

// ─────────────────────────────────────────────────────────────────────────────
// ServiceDesc (manual — replace with proto-generated once protoc is wired)
// ─────────────────────────────────────────────────────────────────────────────

// rawHandler wraps a method fn that accepts raw bytes and returns raw bytes.
type rawHandler func(ctx context.Context, rawReq []byte) ([]byte, error)

func makeUnaryHandler(fn rawHandler) grpc.MethodDesc {
	return grpc.MethodDesc{
		MethodName: "",
		Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, _ grpc.UnaryServerInterceptor) (interface{}, error) {
			var raw json.RawMessage
			if err := dec(&raw); err != nil {
				return nil, err
			}
			return fn(ctx, raw)
		},
	}
}

var _EliteAgentMesh_serviceDesc = grpc.ServiceDesc{
	ServiceName: "mcpmesh.v1.EliteAgentMesh",
	HandlerType: (*interface{})(nil),
	Methods: []grpc.MethodDesc{
		{
			MethodName: "RegisterAgent",
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
				s := srv.(*MeshGRPCServer)
				var raw json.RawMessage
				if err := dec(&raw); err != nil {
					return nil, err
				}
				h := func(ctx context.Context, req interface{}) (interface{}, error) {
					return s.registerAgent(ctx, req.(json.RawMessage))
				}
				if interceptor == nil {
					return h(ctx, raw)
				}
				return interceptor(ctx, raw, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/mcpmesh.v1.EliteAgentMesh/RegisterAgent"}, h)
			},
		},
		{
			MethodName: "DiscoverAgents",
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
				s := srv.(*MeshGRPCServer)
				var raw json.RawMessage
				if err := dec(&raw); err != nil {
					return nil, err
				}
				h := func(ctx context.Context, req interface{}) (interface{}, error) {
					return s.discoverAgents(ctx, req.(json.RawMessage))
				}
				if interceptor == nil {
					return h(ctx, raw)
				}
				return interceptor(ctx, raw, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/mcpmesh.v1.EliteAgentMesh/DiscoverAgents"}, h)
			},
		},
		{
			MethodName: "ExecuteAgent",
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
				s := srv.(*MeshGRPCServer)
				var raw json.RawMessage
				if err := dec(&raw); err != nil {
					return nil, err
				}
				h := func(ctx context.Context, req interface{}) (interface{}, error) {
					return s.executeAgent(ctx, req.(json.RawMessage))
				}
				if interceptor == nil {
					return h(ctx, raw)
				}
				return interceptor(ctx, raw, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/mcpmesh.v1.EliteAgentMesh/ExecuteAgent"}, h)
			},
		},
		{
			MethodName: "Heartbeat",
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
				s := srv.(*MeshGRPCServer)
				var raw json.RawMessage
				if err := dec(&raw); err != nil {
					return nil, err
				}
				h := func(ctx context.Context, req interface{}) (interface{}, error) {
					return s.heartbeat(ctx, req.(json.RawMessage))
				}
				if interceptor == nil {
					return h(ctx, raw)
				}
				return interceptor(ctx, raw, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/mcpmesh.v1.EliteAgentMesh/Heartbeat"}, h)
			},
		},
		{
			MethodName: "ListAgents",
			Handler: func(srv interface{}, ctx context.Context, dec func(interface{}) error, interceptor grpc.UnaryServerInterceptor) (interface{}, error) {
				s := srv.(*MeshGRPCServer)
				var raw json.RawMessage
				if err := dec(&raw); err != nil {
					return nil, err
				}
				h := func(ctx context.Context, req interface{}) (interface{}, error) {
					return s.listAgents(ctx, req.(json.RawMessage))
				}
				if interceptor == nil {
					return h(ctx, raw)
				}
				return interceptor(ctx, raw, &grpc.UnaryServerInfo{Server: srv, FullMethod: "/mcpmesh.v1.EliteAgentMesh/ListAgents"}, h)
			},
		},
	},
	Streams:  []grpc.StreamDesc{},
	Metadata: "proto/elite_agents.proto",
}
