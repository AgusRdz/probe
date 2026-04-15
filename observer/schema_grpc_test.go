package observer

import (
	"testing"

	"google.golang.org/protobuf/types/descriptorpb"
)

func TestProtoFieldToSchema(t *testing.T) {
	tests := []struct {
		name     string
		protoTyp descriptorpb.FieldDescriptorProto_Type
		wantType string
	}{
		{"string", descriptorpb.FieldDescriptorProto_TYPE_STRING, "string"},
		{"bytes", descriptorpb.FieldDescriptorProto_TYPE_BYTES, "string"},
		{"int32", descriptorpb.FieldDescriptorProto_TYPE_INT32, "integer"},
		{"int64", descriptorpb.FieldDescriptorProto_TYPE_INT64, "integer"},
		{"uint32", descriptorpb.FieldDescriptorProto_TYPE_UINT32, "integer"},
		{"uint64", descriptorpb.FieldDescriptorProto_TYPE_UINT64, "integer"},
		{"sint32", descriptorpb.FieldDescriptorProto_TYPE_SINT32, "integer"},
		{"sint64", descriptorpb.FieldDescriptorProto_TYPE_SINT64, "integer"},
		{"fixed32", descriptorpb.FieldDescriptorProto_TYPE_FIXED32, "integer"},
		{"fixed64", descriptorpb.FieldDescriptorProto_TYPE_FIXED64, "integer"},
		{"sfixed32", descriptorpb.FieldDescriptorProto_TYPE_SFIXED32, "integer"},
		{"sfixed64", descriptorpb.FieldDescriptorProto_TYPE_SFIXED64, "integer"},
		{"float", descriptorpb.FieldDescriptorProto_TYPE_FLOAT, "number"},
		{"double", descriptorpb.FieldDescriptorProto_TYPE_DOUBLE, "number"},
		{"bool", descriptorpb.FieldDescriptorProto_TYPE_BOOL, "boolean"},
		{"message", descriptorpb.FieldDescriptorProto_TYPE_MESSAGE, "object"},
		{"group", descriptorpb.FieldDescriptorProto_TYPE_GROUP, "object"},
		{"enum", descriptorpb.FieldDescriptorProto_TYPE_ENUM, "string"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := protoFieldToSchema(tc.protoTyp)
			if got.Type != tc.wantType {
				t.Errorf("protoFieldToSchema(%v) = %q, want %q", tc.protoTyp, got.Type, tc.wantType)
			}
		})
	}
}

// TestReflectGRPC_UnreachableReturnsError verifies that an unreachable address
// returns an error rather than panicking.
func TestReflectGRPC_UnreachableReturnsError(t *testing.T) {
	_, err := ReflectGRPC("localhost:1")
	if err == nil {
		t.Fatal("expected error for unreachable gRPC server, got nil")
	}
}
