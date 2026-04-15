package observer

import (
	"testing"
)

func TestInferXMLBody_NilOnEmpty(t *testing.T) {
	if got := InferXMLBody(nil); got != nil {
		t.Fatalf("expected nil for nil input, got %+v", got)
	}
	if got := InferXMLBody([]byte{}); got != nil {
		t.Fatalf("expected nil for empty input, got %+v", got)
	}
}

func TestInferXMLBody_NilOnInvalidXML(t *testing.T) {
	if got := InferXMLBody([]byte(`<not closed`)); got != nil {
		t.Fatalf("expected nil for invalid XML, got %+v", got)
	}
}

func TestInferXMLBody_SimpleTextElement(t *testing.T) {
	data := []byte(`<name>Alice</name>`)
	got := InferXMLBody(data)
	if got == nil {
		t.Fatal("expected non-nil schema")
	}
	if got.Type != "string" {
		t.Errorf("Type: want string, got %q", got.Type)
	}
}

func TestInferXMLBody_Attributes(t *testing.T) {
	data := []byte(`<user id="42" active="true"><name>Alice</name></user>`)
	got := InferXMLBody(data)
	if got == nil {
		t.Fatal("expected non-nil schema")
	}
	if got.Type != "object" {
		t.Errorf("Type: want object, got %q", got.Type)
	}

	idProp, ok := got.Properties["@id"]
	if !ok {
		t.Fatal("expected '@id' property for attribute")
	}
	if idProp.Type != "string" {
		t.Errorf("@id type: want string, got %q", idProp.Type)
	}
	if !idProp.XMLAttr {
		t.Error("@id: expected XMLAttr=true")
	}

	activeProp, ok := got.Properties["@active"]
	if !ok {
		t.Fatal("expected '@active' property for attribute")
	}
	if activeProp.Type != "string" {
		t.Errorf("@active type: want string, got %q", activeProp.Type)
	}
}

func TestInferXMLBody_NestedElements(t *testing.T) {
	data := []byte(`
<user>
  <name>Alice</name>
  <email>alice@example.com</email>
  <address>
    <street>123 Main St</street>
    <city>Anytown</city>
  </address>
</user>`)
	got := InferXMLBody(data)
	if got == nil {
		t.Fatal("expected non-nil schema")
	}
	if got.Type != "object" {
		t.Errorf("root Type: want object, got %q", got.Type)
	}

	nameProp, ok := got.Properties["name"]
	if !ok {
		t.Fatal("expected 'name' property")
	}
	if nameProp.Type != "string" {
		t.Errorf("name type: want string, got %q", nameProp.Type)
	}

	addrProp, ok := got.Properties["address"]
	if !ok {
		t.Fatal("expected 'address' property")
	}
	if addrProp.Type != "object" {
		t.Errorf("address type: want object, got %q", addrProp.Type)
	}

	streetProp, ok := addrProp.Properties["street"]
	if !ok {
		t.Fatal("expected 'street' in address properties")
	}
	if streetProp.Type != "string" {
		t.Errorf("street type: want string, got %q", streetProp.Type)
	}
}

func TestInferXMLBody_RepeatedChildrenBecomeArray(t *testing.T) {
	data := []byte(`
<users>
  <user><name>Alice</name></user>
  <user><name>Bob</name></user>
</users>`)
	got := InferXMLBody(data)
	if got == nil {
		t.Fatal("expected non-nil schema")
	}

	userProp, ok := got.Properties["user"]
	if !ok {
		t.Fatal("expected 'user' property")
	}
	if userProp.Type != "array" {
		t.Errorf("user type: want array for repeated elements, got %q", userProp.Type)
	}
	if userProp.Items == nil {
		t.Fatal("expected Items on array schema")
	}
	if userProp.Items.Type != "object" {
		t.Errorf("user items type: want object, got %q", userProp.Items.Type)
	}
}

func TestInferXMLBody_AttributeAndChildMix(t *testing.T) {
	data := []byte(`<product id="99"><title>Widget</title></product>`)
	got := InferXMLBody(data)
	if got == nil {
		t.Fatal("expected non-nil schema")
	}
	if got.Type != "object" {
		t.Errorf("Type: want object, got %q", got.Type)
	}
	if _, ok := got.Properties["@id"]; !ok {
		t.Error("expected '@id' attribute property")
	}
	if _, ok := got.Properties["title"]; !ok {
		t.Error("expected 'title' child element property")
	}
}

func TestInferXMLBody_XMLDeclaration(t *testing.T) {
	data := []byte(`<?xml version="1.0" encoding="UTF-8"?><root><value>42</value></root>`)
	got := InferXMLBody(data)
	if got == nil {
		t.Fatal("expected non-nil schema for XML with declaration")
	}
	if got.Type != "object" {
		t.Errorf("Type: want object, got %q", got.Type)
	}
}
