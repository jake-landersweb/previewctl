package domain

import "testing"

func TestParseComposeData_EnvVarPort(t *testing.T) {
	data := []byte(`
services:
  redis:
    image: redis:7-alpine
    ports:
      - "${REDIS_PORT:-6379}:6379"
`)
	services, err := parseComposeData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 1 {
		t.Fatalf("expected 1 service, got %d", len(services))
	}
	redis := services["redis"]
	if redis.Image != "redis:7-alpine" {
		t.Errorf("expected image 'redis:7-alpine', got '%s'", redis.Image)
	}
	if redis.Port != 6379 {
		t.Errorf("expected port 6379, got %d", redis.Port)
	}
	if redis.EnvVar != "REDIS_PORT" {
		t.Errorf("expected env var 'REDIS_PORT', got '%s'", redis.EnvVar)
	}
}

func TestParseComposeData_StaticPort(t *testing.T) {
	data := []byte(`
services:
  redis:
    image: redis:7-alpine
    ports:
      - "6379:6379"
`)
	services, err := parseComposeData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	redis := services["redis"]
	if redis.Port != 6379 {
		t.Errorf("expected port 6379, got %d", redis.Port)
	}
	if redis.EnvVar != "" {
		t.Errorf("expected empty env var, got '%s'", redis.EnvVar)
	}
}

func TestParseComposeData_MultipleServices(t *testing.T) {
	data := []byte(`
services:
  redis:
    image: redis:7-alpine
    ports:
      - "${REDIS_PORT:-6379}:6379"
  postgres:
    image: postgres:16
    ports:
      - "${POSTGRES_PORT:-5432}:5432"
`)
	services, err := parseComposeData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 services, got %d", len(services))
	}
	if services["redis"].Port != 6379 {
		t.Errorf("expected redis port 6379, got %d", services["redis"].Port)
	}
	if services["postgres"].Port != 5432 {
		t.Errorf("expected postgres port 5432, got %d", services["postgres"].Port)
	}
}

func TestParseComposeData_ContainerOnlyPort(t *testing.T) {
	data := []byte(`
services:
  mongo:
    image: mongo:7.0
    ports:
      - "27017"
`)
	services, err := parseComposeData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	mongo := services["mongo"]
	if mongo.Port != 27017 {
		t.Errorf("expected port 27017, got %d", mongo.Port)
	}
	if mongo.EnvVar != "" {
		t.Errorf("expected empty env var, got '%s'", mongo.EnvVar)
	}
}

func TestParseComposeData_NoPorts(t *testing.T) {
	data := []byte(`
services:
  worker:
    image: myapp:latest
`)
	services, err := parseComposeData(data)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if services["worker"].Port != 0 {
		t.Errorf("expected port 0, got %d", services["worker"].Port)
	}
}
