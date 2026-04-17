package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestHapiExtractor_SingleMethod(t *testing.T) {
	dir := t.TempDir()

	src := `
server.route({
    method: 'GET',
    path: '/users',
    handler: async (request, h) => {
        return [];
    }
});
`
	if err := os.WriteFile(filepath.Join(dir, "routes.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &hapiExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.PathPattern == "/users" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GET /users, got: %+v", endpoints)
	}
}

func TestHapiExtractor_ArrayMethods(t *testing.T) {
	dir := t.TempDir()

	src := `
server.route({
    method: ['GET', 'POST'],
    path: '/users',
    handler: async (request, h) => {
        return {};
    }
});
`
	if err := os.WriteFile(filepath.Join(dir, "routes.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &hapiExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		if ep.PathPattern == "/users" {
			found[ep.Method] = true
		}
	}

	if !found["GET"] {
		t.Errorf("expected GET /users, got: %v", found)
	}
	if !found["POST"] {
		t.Errorf("expected POST /users, got: %v", found)
	}
}

func TestHapiExtractor_SingleLine(t *testing.T) {
	dir := t.TempDir()

	src := `server.route({ method: 'DELETE', path: '/users/{id}', handler: deleteUser });`

	if err := os.WriteFile(filepath.Join(dir, "routes.ts"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &hapiExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "DELETE" && ep.PathPattern == "/users/{id}" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected DELETE /users/{id}, got: %+v", endpoints)
	}
}

func TestHapiExtractor_DeprecatedJSDoc(t *testing.T) {
	dir := t.TempDir()

	src := `
/**
 * @deprecated Use /v2/users instead
 * @description List all users
 */
server.route({
    method: 'GET',
    path: '/v1/users',
    handler: listUsers
});
`
	if err := os.WriteFile(filepath.Join(dir, "routes.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &hapiExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	var ep *ScannedEndpoint
	for i := range endpoints {
		if endpoints[i].Method == "GET" && endpoints[i].PathPattern == "/v1/users" {
			ep = &endpoints[i]
			break
		}
	}

	if ep == nil {
		t.Fatalf("expected GET /v1/users, got: %+v", endpoints)
	}
	if !ep.Deprecated {
		t.Errorf("expected Deprecated=true")
	}
	if ep.Description == "" {
		t.Errorf("expected non-empty Description")
	}
}
