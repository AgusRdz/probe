package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestFastAPIDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &fastAPIExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without requirements.txt")
	}

	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("fastapi==0.100.0\nuvicorn"), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect with fastapi in requirements.txt")
	}
}

func TestFastAPIExtractRoutes(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("fastapi"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `
from fastapi import FastAPI
from pydantic import BaseModel

app = FastAPI()

class CreateUserRequest(BaseModel):
    name: str
    email: str
    age: Optional[int]

@app.post("/users", response_model=CreateUserRequest)
async def create_user(body: CreateUserRequest):
    """Create a new user"""
    pass

@app.get("/users/{user_id}")
async def get_user(user_id: int):
    pass
`
	if err := os.WriteFile(filepath.Join(dir, "main.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &fastAPIExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	if len(endpoints) < 2 {
		t.Fatalf("expected at least 2 endpoints, got %d", len(endpoints))
	}

	// Verify POST /users was found.
	found := false
	for _, ep := range endpoints {
		if ep.Method == "POST" && ep.PathPattern == "/users" {
			found = true
			if ep.Description == "" {
				t.Error("expected description from docstring")
			}
		}
	}
	if !found {
		t.Error("POST /users not found")
	}
}

func TestFastAPIExtractPydanticSchema(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("fastapi"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `
from pydantic import BaseModel

class ItemRequest(BaseModel):
    name: str
    price: float
    in_stock: bool

from fastapi import FastAPI
app = FastAPI()

@app.post("/items")
async def create_item(body: ItemRequest):
    pass
`
	if err := os.WriteFile(filepath.Join(dir, "items.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &fastAPIExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	var postItems *ScannedEndpoint
	for i := range endpoints {
		if endpoints[i].Method == "POST" && endpoints[i].PathPattern == "/items" {
			postItems = &endpoints[i]
			break
		}
	}
	if postItems == nil {
		t.Fatal("POST /items not found")
	}
	if postItems.ReqSchema == nil {
		t.Fatal("expected request schema")
	}
	if _, ok := postItems.ReqSchema.Properties["name"]; !ok {
		t.Error("expected 'name' property in schema")
	}
}
