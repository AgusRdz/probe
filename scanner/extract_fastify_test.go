package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestFastifyExtractor_BasicRoutes(t *testing.T) {
	dir := t.TempDir()

	src := `
const fastify = require('fastify')();

fastify.get('/users', async (req, reply) => reply.send([]));
fastify.post('/users', async (req, reply) => reply.send({}));
fastify.put('/users/:id', async (req, reply) => reply.send({}));
fastify.delete('/users/:id', async (req, reply) => reply.send({}));
`
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &fastifyExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{"GET:/users", "POST:/users", "PUT:/users/{id}", "DELETE:/users/{id}"} {
		if !found[want] {
			t.Errorf("expected %s, got: %v", want, found)
		}
	}
}

func TestFastifyExtractor_RouteObject(t *testing.T) {
	dir := t.TempDir()

	src := `
fastify.route({ method: 'GET', url: '/orders', handler: getOrders });
fastify.route({ method: 'POST', url: '/orders', handler: createOrder });
`
	if err := os.WriteFile(filepath.Join(dir, "routes.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &fastifyExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	if !found["GET:/orders"] {
		t.Errorf("expected GET:/orders, got: %v", found)
	}
	if !found["POST:/orders"] {
		t.Errorf("expected POST:/orders, got: %v", found)
	}
}

func TestFastifyExtractor_Deprecated(t *testing.T) {
	dir := t.TempDir()

	src := `
/**
 * @deprecated Use /v2/users instead.
 */
fastify.get('/v1/users', async (req, reply) => reply.send([]));
`
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &fastifyExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if len(endpoints) == 0 {
		t.Fatal("expected at least one endpoint")
	}

	var ep *ScannedEndpoint
	for i := range endpoints {
		if endpoints[i].PathPattern == "/v1/users" {
			ep = &endpoints[i]
			break
		}
	}
	if ep == nil {
		t.Fatalf("GET:/v1/users not found, got: %+v", endpoints)
	}
	if !ep.Deprecated {
		t.Errorf("expected Deprecated=true for /v1/users")
	}
}

func TestFastifyExtractor_EmptyFile(t *testing.T) {
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "empty.js"), []byte(""), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &fastifyExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if len(endpoints) != 0 {
		t.Errorf("expected no endpoints from empty file, got: %+v", endpoints)
	}
}
