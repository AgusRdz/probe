package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestFlaskDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &flaskExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without requirements.txt")
	}

	// FastAPI present — should NOT detect as Flask.
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("fastapi\nflask"), 0644); err != nil {
		t.Fatal(err)
	}
	if ex.Detect(dir) {
		t.Fatal("should not detect Flask when FastAPI is also present")
	}

	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask==2.3.0"), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect flask-only project")
	}
}

func TestFlaskExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `
from flask import Flask
app = Flask(__name__)

@app.route("/users", methods=["GET", "POST"])
def users():
    pass

@app.route("/users/<int:user_id>", methods=["GET"])
def get_user(user_id):
    pass
`
	if err := os.WriteFile(filepath.Join(dir, "app.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &flaskExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// Expect GET /users, POST /users, GET /users/{user_id}
	if len(endpoints) < 3 {
		t.Fatalf("expected at least 3 endpoints, got %d", len(endpoints))
	}

	methods := map[string]bool{}
	for _, ep := range endpoints {
		methods[ep.Method+":"+ep.PathPattern] = true
	}
	for _, want := range []string{"GET:/users", "POST:/users"} {
		if !methods[want] {
			t.Errorf("missing endpoint %s", want)
		}
	}
}

func TestFlaskBlueprintPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("flask"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `
from flask import Blueprint
bp = Blueprint("api", __name__, url_prefix="/api")

@bp.route("/health")
def health():
    pass
`
	if err := os.WriteFile(filepath.Join(dir, "routes.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &flaskExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) == 0 {
		t.Fatal("expected at least 1 endpoint")
	}
	if endpoints[0].PathPattern != "/api/health" {
		t.Errorf("expected /api/health, got %s", endpoints[0].PathPattern)
	}
}
