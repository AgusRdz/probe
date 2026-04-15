package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestExpressExtractor_BasicRoutes(t *testing.T) {
	dir := t.TempDir()

	appJS := `
const express = require('express');
const app = express();
const router = express.Router();

app.get('/users', function(req, res) {
  res.json([]);
});

router.post('/users', function(req, res) {
  res.json({});
});
`
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(appJS), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &expressExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	if len(endpoints) < 2 {
		t.Fatalf("expected at least 2 endpoints, got %d: %+v", len(endpoints), endpoints)
	}

	methodPaths := make(map[string]bool)
	for _, ep := range endpoints {
		methodPaths[ep.Method+":"+ep.PathPattern] = true
	}

	if !methodPaths["GET:/users"] {
		t.Errorf("expected GET:/users, got methods: %v", methodPaths)
	}
	if !methodPaths["POST:/users"] {
		t.Errorf("expected POST:/users, got methods: %v", methodPaths)
	}
}

func TestExpressExtractor_PathParams(t *testing.T) {
	dir := t.TempDir()

	appJS := `
const app = require('express')();
app.get('/users/:id', function(req, res) {});
app.delete('/users/:userId/posts/:postId', function(req, res) {});
`
	if err := os.WriteFile(filepath.Join(dir, "routes.js"), []byte(appJS), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &expressExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	if !found["GET:/users/{id}"] {
		t.Errorf("expected GET:/users/{id}, got: %v", found)
	}
	if !found["DELETE:/users/{userId}/posts/{postId}"] {
		t.Errorf("expected DELETE:/users/{userId}/posts/{postId}, got: %v", found)
	}
}

func TestExpressExtractor_SkipsNodeModules(t *testing.T) {
	dir := t.TempDir()

	// Real route in root.
	appJS := `app.get('/real', function(req, res) {});`
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(appJS), 0644); err != nil {
		t.Fatal(err)
	}

	// Route in node_modules — must be ignored.
	nmDir := filepath.Join(dir, "node_modules", "some-pkg")
	if err := os.MkdirAll(nmDir, 0755); err != nil {
		t.Fatal(err)
	}
	nmJS := `app.get('/should-not-appear', function(req, res) {});`
	if err := os.WriteFile(filepath.Join(nmDir, "index.js"), []byte(nmJS), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &expressExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	for _, ep := range endpoints {
		if ep.PathPattern == "/should-not-appear" {
			t.Errorf("endpoint from node_modules was scanned: %+v", ep)
		}
	}

	found := false
	for _, ep := range endpoints {
		if ep.PathPattern == "/real" && ep.Method == "GET" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected GET:/real to be found, got: %+v", endpoints)
	}
}
