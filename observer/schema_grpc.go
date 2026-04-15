package observer

import (
	"context"
	"fmt"
	"net/url"
	"time"

	grpc_reflection_v1alpha "google.golang.org/grpc/reflection/grpc_reflection_v1alpha"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/descriptorpb"
)

// GRPCService represents a discovered gRPC service with methods and schemas.
type GRPCService struct {
	ServiceName string
	Methods     []GRPCMethod
}

// GRPCMethod represents a single gRPC method.
type GRPCMethod struct {
	Name       string
	InputType  string // proto message name
	OutputType string // proto message name
	ReqSchema  *Schema
	RespSchema *Schema
	Streaming  bool
}

// ReflectGRPC connects to a gRPC server at addr, calls the server reflection API,
// and returns all services with their methods and inferred schemas.
// Uses a 10-second total timeout. Returns an error on connection/reflection failure.
// addr format: "host:port"
func ReflectGRPC(addr string) ([]GRPCService, error) {
	conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("grpc reflect: dial %s: %w", addr, err)
	}
	defer conn.Close() //nolint:errcheck

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	stub := grpc_reflection_v1alpha.NewServerReflectionClient(conn)
	stream, err := stub.ServerReflectionInfo(ctx)
	if err != nil {
		return nil, fmt.Errorf("grpc reflect: open stream: %w", err)
	}

	// List all services.
	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_ListServices{
			ListServices: "",
		},
	}); err != nil {
		return nil, fmt.Errorf("grpc reflect: send ListServices: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("grpc reflect: recv ListServices: %w", err)
	}

	listResp, ok := resp.MessageResponse.(*grpc_reflection_v1alpha.ServerReflectionResponse_ListServicesResponse)
	if !ok {
		return nil, fmt.Errorf("grpc reflect: unexpected response type for ListServices")
	}

	// Build a map of FileDescriptorProto by filename to avoid re-fetching.
	fdCache := map[string]*descriptorpb.FileDescriptorProto{}

	var services []GRPCService

	for _, svc := range listResp.ListServicesResponse.Service {
		// Skip the reflection service itself.
		if svc.Name == "grpc.reflection.v1alpha.ServerReflection" ||
			svc.Name == "grpc.reflection.v1.ServerReflection" {
			continue
		}

		fds, err := fetchFileDescriptors(stream, svc.Name, fdCache)
		if err != nil {
			// Isolate per-service failures — log to stderr and continue.
			fmt.Printf("grpc reflect: fetch descriptors for %s: %v\n", svc.Name, err)
			continue
		}

		methods := extractMethods(svc.Name, fds)
		services = append(services, GRPCService{
			ServiceName: svc.Name,
			Methods:     methods,
		})
	}

	if services == nil {
		return []GRPCService{}, nil
	}
	return services, nil
}

// ReflectGRPCFromTarget parses a target URL and calls ReflectGRPC with host:port.
func ReflectGRPCFromTarget(targetURL string) ([]GRPCService, error) {
	u, err := url.Parse(targetURL)
	if err != nil {
		return nil, fmt.Errorf("grpc reflect: parse target URL: %w", err)
	}
	host := u.Host
	if host == "" {
		return nil, fmt.Errorf("grpc reflect: no host in target URL %q", targetURL)
	}
	return ReflectGRPC(host)
}

// fetchFileDescriptors sends a FileContainingSymbol request for the given service name
// and returns all FileDescriptorProtos in the response (including transitive deps).
func fetchFileDescriptors(
	stream grpc_reflection_v1alpha.ServerReflection_ServerReflectionInfoClient,
	symbol string,
	cache map[string]*descriptorpb.FileDescriptorProto,
) ([]*descriptorpb.FileDescriptorProto, error) {
	if err := stream.Send(&grpc_reflection_v1alpha.ServerReflectionRequest{
		MessageRequest: &grpc_reflection_v1alpha.ServerReflectionRequest_FileContainingSymbol{
			FileContainingSymbol: symbol,
		},
	}); err != nil {
		return nil, fmt.Errorf("send FileContainingSymbol: %w", err)
	}

	resp, err := stream.Recv()
	if err != nil {
		return nil, fmt.Errorf("recv FileContainingSymbol: %w", err)
	}

	fdResp, ok := resp.MessageResponse.(*grpc_reflection_v1alpha.ServerReflectionResponse_FileDescriptorResponse)
	if !ok {
		return nil, fmt.Errorf("unexpected response type for FileContainingSymbol")
	}

	var result []*descriptorpb.FileDescriptorProto
	for _, raw := range fdResp.FileDescriptorResponse.FileDescriptorProto {
		fd := &descriptorpb.FileDescriptorProto{}
		if err := proto.Unmarshal(raw, fd); err != nil {
			continue
		}
		name := fd.GetName()
		if _, seen := cache[name]; seen {
			continue
		}
		cache[name] = fd
		result = append(result, fd)
	}
	return result, nil
}

// extractMethods finds the service descriptor matching serviceName across the
// provided file descriptors and builds a GRPCMethod slice.
func extractMethods(serviceName string, fds []*descriptorpb.FileDescriptorProto) []GRPCMethod {
	// Build a message index for schema inference.
	msgIndex := map[string]*descriptorpb.DescriptorProto{}
	for _, fd := range fds {
		pkg := fd.GetPackage()
		for _, msg := range fd.GetMessageType() {
			indexMessages(pkg, msg, msgIndex)
		}
	}

	// Find the service descriptor.
	for _, fd := range fds {
		pkg := fd.GetPackage()
		for _, svc := range fd.GetService() {
			fqn := svc.GetName()
			if pkg != "" {
				fqn = pkg + "." + svc.GetName()
			}
			if fqn != serviceName {
				continue
			}

			var methods []GRPCMethod
			for _, m := range svc.GetMethod() {
				reqSchema := messageToSchema(m.GetInputType(), msgIndex)
				respSchema := messageToSchema(m.GetOutputType(), msgIndex)
				streaming := m.GetClientStreaming() || m.GetServerStreaming()
				methods = append(methods, GRPCMethod{
					Name:       m.GetName(),
					InputType:  m.GetInputType(),
					OutputType: m.GetOutputType(),
					ReqSchema:  reqSchema,
					RespSchema: respSchema,
					Streaming:  streaming,
				})
			}
			return methods
		}
	}
	return nil
}

// indexMessages recursively adds all messages to the index under their fully-qualified name.
func indexMessages(pkg string, msg *descriptorpb.DescriptorProto, idx map[string]*descriptorpb.DescriptorProto) {
	fqn := msg.GetName()
	if pkg != "" {
		fqn = pkg + "." + msg.GetName()
	}
	idx[fqn] = msg
	// Index nested messages using the parent's fqn as their package.
	for _, nested := range msg.GetNestedType() {
		indexMessages(fqn, nested, idx)
	}
}

// messageToSchema builds a Schema for a proto message type name.
// typeRef is in the form ".package.MessageName" (leading dot from proto).
func messageToSchema(typeRef string, msgIndex map[string]*descriptorpb.DescriptorProto) *Schema {
	if typeRef == "" {
		return &Schema{Type: "object", Description: "grpc"}
	}

	// Strip leading dot from proto fully-qualified type references.
	key := typeRef
	if len(key) > 0 && key[0] == '.' {
		key = key[1:]
	}

	msg, ok := msgIndex[key]
	if !ok {
		return &Schema{Type: "object", Description: "grpc"}
	}

	props := make(map[string]*Schema, len(msg.GetField()))
	for _, f := range msg.GetField() {
		s := protoFieldToSchema(f.GetType())
		sCopy := s
		props[f.GetName()] = &sCopy
	}

	schema := &Schema{
		Type:        "object",
		Description: "grpc",
	}
	if len(props) > 0 {
		schema.Properties = props
	}
	return schema
}

// protoFieldToSchema maps a proto field type to a Schema.
func protoFieldToSchema(t descriptorpb.FieldDescriptorProto_Type) Schema {
	switch t {
	case descriptorpb.FieldDescriptorProto_TYPE_STRING,
		descriptorpb.FieldDescriptorProto_TYPE_BYTES:
		return Schema{Type: "string"}

	case descriptorpb.FieldDescriptorProto_TYPE_INT32,
		descriptorpb.FieldDescriptorProto_TYPE_INT64,
		descriptorpb.FieldDescriptorProto_TYPE_UINT32,
		descriptorpb.FieldDescriptorProto_TYPE_UINT64,
		descriptorpb.FieldDescriptorProto_TYPE_SINT32,
		descriptorpb.FieldDescriptorProto_TYPE_SINT64,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_FIXED64,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED32,
		descriptorpb.FieldDescriptorProto_TYPE_SFIXED64:
		return Schema{Type: "integer"}

	case descriptorpb.FieldDescriptorProto_TYPE_FLOAT,
		descriptorpb.FieldDescriptorProto_TYPE_DOUBLE:
		return Schema{Type: "number"}

	case descriptorpb.FieldDescriptorProto_TYPE_BOOL:
		return Schema{Type: "boolean"}

	case descriptorpb.FieldDescriptorProto_TYPE_MESSAGE,
		descriptorpb.FieldDescriptorProto_TYPE_GROUP:
		return Schema{Type: "object"}

	case descriptorpb.FieldDescriptorProto_TYPE_ENUM:
		return Schema{Type: "string"}

	default:
		return Schema{Type: "string"}
	}
}
