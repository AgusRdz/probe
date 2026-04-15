package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestNextJSExtractor_PagesRouter(t *testing.T) {
	dir := t.TempDir()

	// Create pages/api/users/[id].ts
	routeDir := filepath.Join(dir, "pages", "api", "users")
	if err := os.MkdirAll(routeDir, 0755); err != nil {
		t.Fatal(err)
	}

	handler := `
import { NextApiRequest, NextApiResponse } from 'next';

export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  if (req.method === 'GET') {
    res.json({ id: req.query.id });
  } else if (req.method === 'POST') {
    res.status(201).json({});
  }
}
`
	if err := os.WriteFile(filepath.Join(routeDir, "[id].ts"), []byte(handler), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nextjsExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	// req.method === 'GET' and req.method === 'POST' are detected.
	if !found["GET:/api/users/{id}"] {
		t.Errorf("expected GET:/api/users/{id}, got: %v", found)
	}
	if !found["POST:/api/users/{id}"] {
		t.Errorf("expected POST:/api/users/{id}, got: %v", found)
	}
}

func TestNextJSExtractor_AppRouter(t *testing.T) {
	dir := t.TempDir()

	// Create app/api/users/[id]/route.ts with export GET
	routeDir := filepath.Join(dir, "app", "api", "users", "[id]")
	if err := os.MkdirAll(routeDir, 0755); err != nil {
		t.Fatal(err)
	}

	routeTS := `
import { NextRequest, NextResponse } from 'next/server';

export async function GET(request: NextRequest, { params }: { params: { id: string } }) {
  return NextResponse.json({ id: params.id });
}

export async function PUT(request: NextRequest) {
  const body = await request.json();
  return NextResponse.json(body);
}
`
	if err := os.WriteFile(filepath.Join(routeDir, "route.ts"), []byte(routeTS), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nextjsExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := make(map[string]bool)
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	if !found["GET:/api/users/{id}"] {
		t.Errorf("expected GET:/api/users/{id}, got: %v", found)
	}
	if !found["PUT:/api/users/{id}"] {
		t.Errorf("expected PUT:/api/users/{id}, got: %v", found)
	}
}
