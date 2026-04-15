package observer

import (
	"testing"
)

func TestExtract_EmptyPair(t *testing.T) {
	pair := CapturedPair{}
	req, resp := Extract(pair)
	if req != nil || resp != nil {
		t.Errorf("Extract(empty) = (%v, %v), want (nil, nil)", req, resp)
	}
}

func TestExtract_JSONReqBody(t *testing.T) {
	pair := CapturedPair{
		ReqContentType: "application/json",
		ReqBody:        []byte(`{"name":"alice","age":30}`),
	}
	req, _ := Extract(pair)
	if req == nil {
		t.Fatal("Extract: reqSchema is nil, want non-nil")
	}
	if req.Type != "object" {
		t.Errorf("reqSchema.Type = %q, want %q", req.Type, "object")
	}
}

func TestExtract_InvalidJSONWithJSONContentType(t *testing.T) {
	pair := CapturedPair{
		ReqContentType: "application/json",
		ReqBody:        []byte(`not json at all`),
	}
	req, _ := Extract(pair)
	if req != nil {
		t.Errorf("Extract: reqSchema = %+v, want nil for invalid JSON body", req)
	}
}

func TestDetectProtocol_GraphQL(t *testing.T) {
	body := []byte(`{"query":"{ users { id } }"}`)
	proto := DetectProtocol("application/json", body)
	if proto != "graphql" {
		t.Errorf("DetectProtocol = %q, want %q", proto, "graphql")
	}
}

func TestDetectProtocol_GraphQLContentType(t *testing.T) {
	proto := DetectProtocol("application/graphql", []byte(`{ users { id } }`))
	if proto != "graphql" {
		t.Errorf("DetectProtocol = %q, want %q", proto, "graphql")
	}
}

func TestDetectProtocol_Form(t *testing.T) {
	proto := DetectProtocol("application/x-www-form-urlencoded", []byte(`name=alice&age=30`))
	if proto != "form" {
		t.Errorf("DetectProtocol = %q, want %q", proto, "form")
	}
}

func TestDetectProtocol_GRPC(t *testing.T) {
	proto := DetectProtocol("application/grpc", nil)
	if proto != "grpc" {
		t.Errorf("DetectProtocol = %q, want %q", proto, "grpc")
	}
}

func TestDetectProtocol_REST(t *testing.T) {
	proto := DetectProtocol("application/json", []byte(`{"id":1}`))
	if proto != "rest" {
		t.Errorf("DetectProtocol = %q, want %q", proto, "rest")
	}
}

func TestExtract_FormContentType(t *testing.T) {
	pair := CapturedPair{
		ReqContentType: "application/x-www-form-urlencoded",
		ReqBody:        []byte(`name=alice&age=30`),
	}
	req, _ := Extract(pair)
	if req == nil {
		t.Fatal("Extract: reqSchema is nil for form body")
	}
	if req.Type != "object" {
		t.Errorf("reqSchema.Type = %q, want %q", req.Type, "object")
	}
}

func TestExtract_GRPCContentType(t *testing.T) {
	pair := CapturedPair{
		ReqContentType: "application/grpc",
		ReqBody:        []byte{0x00, 0x01, 0x02}, // arbitrary binary
	}
	req, _ := Extract(pair)
	if req == nil {
		t.Fatal("Extract: reqSchema is nil for grpc body")
	}
	if req.Description != "grpc" {
		t.Errorf("reqSchema.Description = %q, want %q", req.Description, "grpc")
	}
}

func TestExtract_GraphQLOverride(t *testing.T) {
	pair := CapturedPair{
		ReqContentType:  "application/json",
		RespContentType: "application/json",
		ReqBody:         []byte(`{"query":"{ users { id } }"}`),
		RespBody:        []byte(`{"data":{"users":[{"id":1}]}}`),
	}
	req, resp := Extract(pair)
	if req == nil {
		t.Fatal("Extract: reqSchema is nil for graphql body")
	}
	if req.Description != "graphql" {
		t.Errorf("reqSchema.Description = %q, want %q", req.Description, "graphql")
	}
	if resp == nil {
		t.Fatal("Extract: respSchema is nil for graphql response")
	}
	if resp.Description != "graphql" {
		t.Errorf("respSchema.Description = %q, want %q", resp.Description, "graphql")
	}
}
