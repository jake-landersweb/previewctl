package domain

import "testing"

func testContext() *TemplateContext {
	return &TemplateContext{
		ServicePorts: PortMap{
			"backend": 8042,
			"web":     3042,
		},
		InfraPorts: PortMap{
			"redis": 6421,
		},
		Databases: map[string]*DatabaseInfo{
			"main": {
				Host:             "localhost",
				Port:             5500,
				User:             "postgres",
				Password:         "secret",
				Database:         "wt_feat_auth",
				ConnectionString: "postgresql://postgres:secret@localhost:5500/wt_feat_auth",
			},
		},
	}
}

func TestRenderTemplate_ServicePort(t *testing.T) {
	ctx := testContext()

	result, err := RenderTemplate("http://localhost:{{services.backend.port}}/api", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "http://localhost:8042/api" {
		t.Errorf("expected 'http://localhost:8042/api', got '%s'", result)
	}
}

func TestRenderTemplate_InfraPort(t *testing.T) {
	ctx := testContext()

	result, err := RenderTemplate("{{infrastructure.redis.port}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "6421" {
		t.Errorf("expected '6421', got '%s'", result)
	}
}

func TestRenderTemplate_MultiplePorts(t *testing.T) {
	ctx := testContext()

	result, err := RenderTemplate("{{services.backend.port}},{{services.web.port}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "8042,3042" {
		t.Errorf("expected '8042,3042', got '%s'", result)
	}
}

func TestRenderTemplate_DatabaseConnectionString(t *testing.T) {
	ctx := testContext()

	result, err := RenderTemplate("{{core.databases.main.connection_string}}", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "postgresql://postgres:secret@localhost:5500/wt_feat_auth" {
		t.Errorf("unexpected result: %s", result)
	}
}

func TestRenderTemplate_DatabaseFields(t *testing.T) {
	ctx := testContext()

	tests := []struct {
		tmpl     string
		expected string
	}{
		{"{{core.databases.main.host}}", "localhost"},
		{"{{core.databases.main.port}}", "5500"},
		{"{{core.databases.main.user}}", "postgres"},
		{"{{core.databases.main.password}}", "secret"},
		{"{{core.databases.main.database}}", "wt_feat_auth"},
	}

	for _, tt := range tests {
		result, err := RenderTemplate(tt.tmpl, ctx)
		if err != nil {
			t.Errorf("template '%s': unexpected error: %v", tt.tmpl, err)
			continue
		}
		if result != tt.expected {
			t.Errorf("template '%s': expected '%s', got '%s'", tt.tmpl, tt.expected, result)
		}
	}
}

func TestRenderTemplate_UnknownService(t *testing.T) {
	ctx := testContext()

	_, err := RenderTemplate("{{services.unknown.port}}", ctx)
	if err == nil {
		t.Fatal("expected error for unknown service")
	}
}

func TestRenderTemplate_UnknownInfra(t *testing.T) {
	ctx := testContext()

	_, err := RenderTemplate("{{infrastructure.unknown.port}}", ctx)
	if err == nil {
		t.Fatal("expected error for unknown infra")
	}
}

func TestRenderTemplate_UnknownDatabase(t *testing.T) {
	ctx := testContext()

	_, err := RenderTemplate("{{core.databases.unknown.host}}", ctx)
	if err == nil {
		t.Fatal("expected error for unknown database")
	}
}

func TestRenderTemplate_UnknownNamespace(t *testing.T) {
	ctx := testContext()

	_, err := RenderTemplate("{{foo.bar}}", ctx)
	if err == nil {
		t.Fatal("expected error for unknown namespace")
	}
}

func TestRenderTemplate_NoVars(t *testing.T) {
	ctx := testContext()

	result, err := RenderTemplate("plain string", ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "plain string" {
		t.Errorf("expected 'plain string', got '%s'", result)
	}
}

func TestRenderEnvMap(t *testing.T) {
	ctx := testContext()

	envMap := map[string]string{
		"PORT":         "{{services.backend.port}}",
		"DATABASE_URL": "{{core.databases.main.connection_string}}",
		"REDIS":        "{{infrastructure.redis.port}}",
		"STATIC":       "no-template-here",
	}

	result, err := RenderEnvMap(envMap, ctx)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result["PORT"] != "8042" {
		t.Errorf("expected PORT '8042', got '%s'", result["PORT"])
	}
	if result["DATABASE_URL"] != "postgresql://postgres:secret@localhost:5500/wt_feat_auth" {
		t.Errorf("unexpected DATABASE_URL: %s", result["DATABASE_URL"])
	}
	if result["REDIS"] != "6421" {
		t.Errorf("expected REDIS '6421', got '%s'", result["REDIS"])
	}
	if result["STATIC"] != "no-template-here" {
		t.Errorf("expected STATIC unchanged, got '%s'", result["STATIC"])
	}
}
