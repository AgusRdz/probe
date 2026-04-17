package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestKoaExtractor_BasicRoutes(t *testing.T) {
	dir := t.TempDir()

	src := `
const Koa = require('koa');
const Router = require('@koa/router');

const app = new Koa();
const router = new Router();

router.get('/products', ctx => { ctx.body = []; });
router.post('/products', ctx => { ctx.body = {}; });
router.delete('/products/:id', ctx => { ctx.body = {}; });

app.use(router.routes());
`
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &koaExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{"GET:/products", "POST:/products", "DELETE:/products/{id}"} {
		if !found[want] {
			t.Errorf("expected %s, got: %v", want, found)
		}
	}
}

func TestKoaExtractor_RouterPrefix(t *testing.T) {
	dir := t.TempDir()

	src := `
const Router = require('@koa/router');
const router = new Router({ prefix: '/api' });

router.get('/users', ctx => { ctx.body = []; });
router.post('/users', ctx => { ctx.body = {}; });
`
	if err := os.WriteFile(filepath.Join(dir, "routes.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &koaExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	if !found["GET:/api/users"] {
		t.Errorf("expected GET:/api/users with prefix, got: %v", found)
	}
	if !found["POST:/api/users"] {
		t.Errorf("expected POST:/api/users with prefix, got: %v", found)
	}
}

func TestKoaExtractor_MultipleMethods(t *testing.T) {
	dir := t.TempDir()

	src := `
const router = new Router();
router.get('/orders', ctx => {});
router.post('/orders', ctx => {});
router.put('/orders/:id', ctx => {});
router.patch('/orders/:id', ctx => {});
router.delete('/orders/:id', ctx => {});
`
	if err := os.WriteFile(filepath.Join(dir, "orders.js"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &koaExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{
		"GET:/orders",
		"POST:/orders",
		"PUT:/orders/{id}",
		"PATCH:/orders/{id}",
		"DELETE:/orders/{id}",
	} {
		if !found[want] {
			t.Errorf("expected %s, got: %v", want, found)
		}
	}
}
