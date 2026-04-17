package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestSymfonyDetect(t *testing.T) {
	dir := t.TempDir()
	ex := &symfonyExtractor{}

	if ex.Detect(dir) {
		t.Fatal("should not detect without composer.json")
	}

	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"symfony/framework-bundle":"^6.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	if !ex.Detect(dir) {
		t.Fatal("should detect symfony/framework-bundle in composer.json")
	}
}

func TestSymfonyExtractPHP8Attribute(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"symfony/framework-bundle":"^6.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src", "Controller")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `<?php
namespace App\Controller;

use Symfony\Component\Routing\Annotation\Route;

class UserController
{
    #[Route('/users', name: 'users_index', methods: ['GET'])]
    public function index(): Response {}

    #[Route('/users/{id}', name: 'users_show', methods: ['GET'])]
    public function show(int $id): Response {}
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "UserController.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &symfonyExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{"GET:/users", "GET:/users/{id}"} {
		if !found[want] {
			t.Errorf("missing %s; found: %v", want, found)
		}
	}
}

func TestSymfonyExtractMultipleMethods(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"symfony/framework-bundle":"^6.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src", "Controller")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `<?php
class PostController
{
    #[Route('/posts', name: 'posts_create', methods: ['GET', 'POST'])]
    public function create(): Response {}
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "PostController.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &symfonyExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{"GET:/posts", "POST:/posts"} {
		if !found[want] {
			t.Errorf("missing %s; found: %v", want, found)
		}
	}
}

func TestSymfonyExtractClassLevelPrefix(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"symfony/framework-bundle":"^6.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src", "Controller")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `<?php
#[Route('/api')]
class ApiController
{
    #[Route('/users', methods: ['GET'])]
    public function users(): Response {}
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "ApiController.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &symfonyExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.PathPattern == "/api/users" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected GET /api/users; got: %v", endpoints)
	}
}

func TestSymfonyExtractLegacyAnnotation(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "composer.json"), []byte(`{"require":{"symfony/framework-bundle":"^6.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}

	srcDir := filepath.Join(dir, "src", "Controller")
	if err := os.MkdirAll(srcDir, 0755); err != nil {
		t.Fatal(err)
	}

	src := `<?php
class LegacyController
{
    /**
     * @Route("/legacy/items", methods={"GET","POST"})
     */
    public function items(): Response {}
}
`
	if err := os.WriteFile(filepath.Join(srcDir, "LegacyController.php"), []byte(src), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &symfonyExtractor{}
	endpoints, err := ex.Extract(dir, &config.ScanConfig{})
	if err != nil {
		t.Fatal(err)
	}

	found := map[string]bool{}
	for _, ep := range endpoints {
		found[ep.Method+":"+ep.PathPattern] = true
	}

	for _, want := range []string{"GET:/legacy/items", "POST:/legacy/items"} {
		if !found[want] {
			t.Errorf("missing %s; found: %v", want, found)
		}
	}
}
