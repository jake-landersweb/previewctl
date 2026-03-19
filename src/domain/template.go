package domain

import (
	"fmt"
	"regexp"
	"strings"
)

var templateVarPattern = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// TemplateContext holds the values available for template substitution.
type TemplateContext struct {
	ServicePorts   PortMap
	InfraPorts     PortMap
	CoreOutputs    map[string]map[string]string
	CurrentService string // set per-service during rendering, enables {{self.port}}
}

// RenderTemplate replaces {{var}} placeholders in a string with values from the context.
// Supported variable patterns:
//   - {{services.<name>.port}} — allocated port for an application service
//   - {{infrastructure.<name>.port}} — allocated port for an infrastructure service
//   - {{core.<service>.<OUTPUT>}} — output value from a core service
func RenderTemplate(tmpl string, ctx *TemplateContext) (string, error) {
	var renderErr error

	result := templateVarPattern.ReplaceAllStringFunc(tmpl, func(match string) string {
		if renderErr != nil {
			return match
		}

		varPath := strings.TrimSpace(match[2 : len(match)-2])
		parts := strings.Split(varPath, ".")

		value, err := resolveVar(parts, ctx)
		if err != nil {
			renderErr = fmt.Errorf("template variable '%s': %w", varPath, err)
			return match
		}
		return value
	})

	if renderErr != nil {
		return "", renderErr
	}
	return result, nil
}

// RenderEnvMap renders all template variables in a map of env vars.
func RenderEnvMap(envMap map[string]string, ctx *TemplateContext) (map[string]string, error) {
	rendered := make(map[string]string, len(envMap))
	for key, val := range envMap {
		result, err := RenderTemplate(val, ctx)
		if err != nil {
			return nil, fmt.Errorf("env var '%s': %w", key, err)
		}
		rendered[key] = result
	}
	return rendered, nil
}

func resolveVar(parts []string, ctx *TemplateContext) (string, error) {
	if len(parts) == 0 {
		return "", fmt.Errorf("empty variable path")
	}

	switch parts[0] {
	case "self":
		// Shorthand: {{self.port}} resolves to the current service's port
		if ctx.CurrentService == "" {
			return "", fmt.Errorf("'self' can only be used inside a service env block")
		}
		if len(parts) != 2 || parts[1] != "port" {
			return "", fmt.Errorf("expected self.port, got %s", strings.Join(parts, "."))
		}
		port, ok := ctx.ServicePorts[ctx.CurrentService]
		if !ok {
			return "", fmt.Errorf("no port allocated for current service '%s'", ctx.CurrentService)
		}
		return fmt.Sprintf("%d", port), nil

	case "services":
		if len(parts) != 3 || parts[2] != "port" {
			return "", fmt.Errorf("expected services.<name>.port, got %s", strings.Join(parts, "."))
		}
		port, ok := ctx.ServicePorts[parts[1]]
		if !ok {
			return "", fmt.Errorf("unknown service '%s'", parts[1])
		}
		return fmt.Sprintf("%d", port), nil

	case "infrastructure":
		if len(parts) != 3 || parts[2] != "port" {
			return "", fmt.Errorf("expected infrastructure.<name>.port, got %s", strings.Join(parts, "."))
		}
		port, ok := ctx.InfraPorts[parts[1]]
		if !ok {
			return "", fmt.Errorf("unknown infrastructure service '%s'", parts[1])
		}
		return fmt.Sprintf("%d", port), nil

	case "core":
		return resolveCoreVar(parts[1:], ctx)

	default:
		return "", fmt.Errorf("unknown namespace '%s'", parts[0])
	}
}

func resolveCoreVar(parts []string, ctx *TemplateContext) (string, error) {
	if len(parts) != 2 {
		return "", fmt.Errorf("expected core.<service>.<output>")
	}
	svc, ok := ctx.CoreOutputs[parts[0]]
	if !ok {
		return "", fmt.Errorf("unknown core service '%s'", parts[0])
	}
	val, ok := svc[parts[1]]
	if !ok {
		return "", fmt.Errorf("unknown output '%s' for core service '%s'", parts[1], parts[0])
	}
	return val, nil
}
