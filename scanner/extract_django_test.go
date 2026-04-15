package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestDjangoDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &djangoExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without requirements.txt")
	}

	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("django>=4.0"), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect with django in requirements.txt")
	}
}

func TestDjangoExtractURLPatterns(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("django"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `
from django.urls import path
from .views import UserListView, UserDetailView

urlpatterns = [
    path("users/", UserListView.as_view()),
    path("users/<int:pk>/", UserDetailView.as_view()),
]
`
	if err := os.WriteFile(filepath.Join(dir, "urls.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &djangoExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// Expect GET+POST /users/ and GET+POST /users/{pk}/
	if len(endpoints) < 4 {
		t.Fatalf("expected at least 4 endpoints, got %d", len(endpoints))
	}

	paths := map[string]bool{}
	for _, ep := range endpoints {
		paths[ep.PathPattern] = true
	}
	if !paths["/users/"] {
		t.Error("expected /users/ path")
	}
}

func TestDjangoExtractRouterRegister(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("django"), 0644); err != nil {
		t.Fatal(err)
	}

	src := `
from rest_framework.routers import DefaultRouter
router = DefaultRouter()
router.register("users", UserViewSet)
urlpatterns = router.urls
`
	if err := os.WriteFile(filepath.Join(dir, "urls.py"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &djangoExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	// Should generate 6 CRUD routes.
	if len(endpoints) < 6 {
		t.Fatalf("expected at least 6 CRUD endpoints from router.register, got %d", len(endpoints))
	}
}
