package domain

import (
	"context"
	"testing"
)

func TestParseHookOutput_Basic(t *testing.T) {
	output := `CONNECTION_STRING=postgresql://localhost:5432/mydb
DB_HOST=localhost
DB_PORT=5432`

	result := parseHookOutput(output)
	if result["CONNECTION_STRING"] != "postgresql://localhost:5432/mydb" {
		t.Errorf("unexpected CONNECTION_STRING: %s", result["CONNECTION_STRING"])
	}
	if result["DB_HOST"] != "localhost" {
		t.Errorf("unexpected DB_HOST: %s", result["DB_HOST"])
	}
	if result["DB_PORT"] != "5432" {
		t.Errorf("unexpected DB_PORT: %s", result["DB_PORT"])
	}
}

func TestParseHookOutput_SkipsBlanksAndComments(t *testing.T) {
	output := `# This is a comment
KEY1=value1

# Another comment

KEY2=value2
`
	result := parseHookOutput(output)
	if len(result) != 2 {
		t.Fatalf("expected 2 outputs, got %d: %v", len(result), result)
	}
	if result["KEY1"] != "value1" {
		t.Errorf("unexpected KEY1: %s", result["KEY1"])
	}
}

func TestParseHookOutput_ValueWithEquals(t *testing.T) {
	output := `CONNECTION_STRING=postgresql://user:pass@host:5432/db?sslmode=require`
	result := parseHookOutput(output)
	if result["CONNECTION_STRING"] != "postgresql://user:pass@host:5432/db?sslmode=require" {
		t.Errorf("unexpected: %s", result["CONNECTION_STRING"])
	}
}

func TestParseHookOutput_SkipsInvalidLines(t *testing.T) {
	output := `GOOD=value
just some log output
ALSO_GOOD=another`
	result := parseHookOutput(output)
	if len(result) != 2 {
		t.Fatalf("expected 2 outputs, got %d: %v", len(result), result)
	}
}

func TestExecuteCoreHook_CapturesOutput(t *testing.T) {
	outputs, err := ExecuteCoreHook(
		context.Background(),
		`echo "DB_HOST=localhost"; echo "DB_PORT=5432"`,
		[]string{"DB_HOST", "DB_PORT"},
		nil,
		t.TempDir(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["DB_HOST"] != "localhost" {
		t.Errorf("expected DB_HOST=localhost, got %s", outputs["DB_HOST"])
	}
	if outputs["DB_PORT"] != "5432" {
		t.Errorf("expected DB_PORT=5432, got %s", outputs["DB_PORT"])
	}
}

func TestExecuteCoreHook_MissingOutputError(t *testing.T) {
	_, err := ExecuteCoreHook(
		context.Background(),
		`echo "DB_HOST=localhost"`,
		[]string{"DB_HOST", "DB_PORT", "CONNECTION_STRING"},
		nil,
		t.TempDir(),
	)
	if err == nil {
		t.Fatal("expected error for missing outputs")
	}
	if !contains(err.Error(), "DB_PORT") || !contains(err.Error(), "CONNECTION_STRING") {
		t.Errorf("expected missing output names in error, got: %v", err)
	}
}

func TestExecuteCoreHook_NoOutputsRequired(t *testing.T) {
	outputs, err := ExecuteCoreHook(
		context.Background(),
		`echo "done"`,
		nil,
		nil,
		t.TempDir(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(outputs) != 0 {
		// "done" doesn't have = so it's skipped
		t.Errorf("expected 0 outputs, got %d", len(outputs))
	}
}

func TestExecuteCoreHook_ScriptFailure(t *testing.T) {
	_, err := ExecuteCoreHook(
		context.Background(),
		`exit 1`,
		nil,
		nil,
		t.TempDir(),
	)
	if err == nil {
		t.Fatal("expected error for failed script")
	}
}

func TestExecuteCoreHook_EnvVarsPassedThrough(t *testing.T) {
	outputs, err := ExecuteCoreHook(
		context.Background(),
		`echo "RESULT=$MY_VAR"`,
		[]string{"RESULT"},
		[]string{"MY_VAR=hello_world"},
		t.TempDir(),
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["RESULT"] != "hello_world" {
		t.Errorf("expected RESULT=hello_world, got %s", outputs["RESULT"])
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
