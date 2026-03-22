package mcpmesh

import (
	"encoding/json"

	"google.golang.org/grpc/encoding"
)

// jsonCodec is a gRPC codec that uses JSON for wire encoding instead of
// protobuf. It is registered globally in init() so that any gRPC connection
// created in this process uses JSON transport.
//
// This allows the MeshGRPCServer to receive and send json.RawMessage values
// without requiring protoc-generated code. Once the generated stubs are
// available (from proto/elite_agents.proto), switch the server to the default
// protobuf codec by removing this registration.
type jsonCodec struct{}

func (jsonCodec) Marshal(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

func (jsonCodec) Unmarshal(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}

func (jsonCodec) Name() string {
	return "proto" // override the default "proto" codec slot
}

func init() {
	encoding.RegisterCodec(jsonCodec{})
}
