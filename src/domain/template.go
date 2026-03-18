package domain

import (
	"fmt"
	"regexp"
	"strings"
)

var templateVarPattern = regexp.MustCompile(`\{\{([^}]+)\}\}`)

// TemplateContext holds the values available for template substitution.
type TemplateContext struct {
	Ports     PortMap
	Databases map[string]*DatabaseInfo
}

// RenderTemplate replaces {{var}} placeholders in a string with values from the context.
// Supported variable patterns:
//   - {{ports.<service>}} — allocated port for a service
//   - {{core.databases.<name>.connectionString}} — database connection string
//   - {{core.databases.<name>.host}} — database host
//   - {{core.databases.<name>.port}} — database port
//   - {{core.databases.<name>.user}} — database user
//   - {{core.databases.<name>.password}} — database password
//   - {{core.databases.<name>.database}} — database name
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
	case "ports":
		if len(parts) != 2 {
			return "", fmt.Errorf("expected ports.<service>, got %s", strings.Join(parts, "."))
		}
		port, ok := ctx.Ports[parts[1]]
		if !ok {
			return "", fmt.Errorf("unknown service '%s'", parts[1])
		}
		return fmt.Sprintf("%d", port), nil

	case "core":
		return resolveCoreVar(parts[1:], ctx)

	default:
		return "", fmt.Errorf("unknown namespace '%s'", parts[0])
	}
}

func resolveCoreVar(parts []string, ctx *TemplateContext) (string, error) {
	if len(parts) < 3 {
		return "", fmt.Errorf("expected core.databases.<name>.<field>")
	}

	if parts[0] != "databases" {
		return "", fmt.Errorf("unknown core type '%s'", parts[0])
	}

	dbName := parts[1]
	db, ok := ctx.Databases[dbName]
	if !ok {
		return "", fmt.Errorf("unknown database '%s'", dbName)
	}

	field := parts[2]
	switch field {
	case "connectionString":
		return db.ConnectionString, nil
	case "host":
		return db.Host, nil
	case "port":
		return fmt.Sprintf("%d", db.Port), nil
	case "user":
		return db.User, nil
	case "password":
		return db.Password, nil
	case "database":
		return db.Database, nil
	default:
		return "", fmt.Errorf("unknown database field '%s'", field)
	}
}
