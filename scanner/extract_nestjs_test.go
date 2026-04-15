package scanner

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/AgusRdz/probe/config"
)

func TestNestJSExtractor_ControllerRoutes(t *testing.T) {
	dir := t.TempDir()

	controllerTS := `
import { Controller, Get } from '@nestjs/common';

@Controller('/users')
export class UsersController {
  @Get('/:id')
  getUser(@Param('id') id: string) {
    return {};
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "users.controller.ts"), []byte(controllerTS), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nestjsExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	found := false
	for _, ep := range endpoints {
		if ep.Method == "GET" && ep.PathPattern == "/users/{id}" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected GET /users/{id}, got: %+v", endpoints)
	}
}

func TestNestJSExtractor_BodyDTO(t *testing.T) {
	dir := t.TempDir()

	controllerTS := `
import { Controller, Post, Body } from '@nestjs/common';

export class CreateUserDto {
  name: string;
  email: string;
  age: number;
}

@Controller('/users')
export class UsersController {
  @Post()
  createUser(@Body() dto: CreateUserDto) {
    return {};
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "users.controller.ts"), []byte(controllerTS), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nestjsExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	var postEp *ScannedEndpoint
	for i := range endpoints {
		if endpoints[i].Method == "POST" && endpoints[i].PathPattern == "/users" {
			postEp = &endpoints[i]
			break
		}
	}

	if postEp == nil {
		t.Fatalf("expected POST /users, got: %+v", endpoints)
	}

	if postEp.ReqSchema == nil {
		t.Fatal("expected ReqSchema to be non-nil")
	}

	if postEp.ReqSchema.Type != "object" {
		t.Errorf("expected ReqSchema.Type = 'object', got %q", postEp.ReqSchema.Type)
	}

	if len(postEp.ReqSchema.Properties) == 0 {
		t.Errorf("expected ReqSchema.Properties to be non-empty")
	}
}

func TestNestJSExtractor_ApiTags(t *testing.T) {
	dir := t.TempDir()

	controllerTS := `
import { Controller, Get } from '@nestjs/common';
import { ApiTags } from '@nestjs/swagger';

@ApiTags('users')
@Controller('/users')
export class UsersController {
  @Get()
  findAll() {
    return [];
  }
}
`
	if err := os.WriteFile(filepath.Join(dir, "users.controller.ts"), []byte(controllerTS), 0644); err != nil {
		t.Fatal(err)
	}

	ex := &nestjsExtractor{}
	cfg := &config.ScanConfig{}
	endpoints, err := ex.Extract(dir, cfg)
	if err != nil {
		t.Fatalf("Extract error: %v", err)
	}

	var getEp *ScannedEndpoint
	for i := range endpoints {
		if endpoints[i].Method == "GET" && endpoints[i].PathPattern == "/users" {
			getEp = &endpoints[i]
			break
		}
	}

	if getEp == nil {
		t.Fatalf("expected GET /users, got: %+v", endpoints)
	}

	hasUsersTag := false
	for _, tag := range getEp.Tags {
		if tag == "users" {
			hasUsersTag = true
			break
		}
	}
	if !hasUsersTag {
		t.Errorf("expected Tags to contain 'users', got: %v", getEp.Tags)
	}
}
