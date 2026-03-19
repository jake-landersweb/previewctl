package domain

import (
	"fmt"
	"os"
	"regexp"
	"strconv"

	"gopkg.in/yaml.v3"
)

// composeFile represents the subset of a docker compose file we care about.
type composeFile struct {
	Services map[string]composeService `yaml:"services"`
}

// composeService represents a single service in a compose file.
type composeService struct {
	Image string   `yaml:"image"`
	Ports []string `yaml:"ports"`
}

// InfraService holds parsed infrastructure service information from a compose file.
type InfraService struct {
	Name   string
	Image  string
	Port   int
	EnvVar string // e.g., "REDIS_PORT" (extracted from ${REDIS_PORT:-6379} patterns)
}

// envVarPattern matches ${VAR_NAME:-default} in port mappings.
var envVarPattern = regexp.MustCompile(`^\$\{([A-Z_][A-Z0-9_]*):-(\d+)\}:(\d+)$`)

// staticPortPattern matches simple port mappings like "6379:6379".
var staticPortPattern = regexp.MustCompile(`^(\d+):(\d+)$`)

// ParseComposeFile reads a docker compose YAML file and extracts infrastructure
// service definitions including service names, images, and port mappings.
func ParseComposeFile(path string) (map[string]InfraService, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading compose file: %w", err)
	}
	return parseComposeData(data)
}

// parseComposeData parses compose file bytes into InfraService definitions.
func parseComposeData(data []byte) (map[string]InfraService, error) {
	var cf composeFile
	if err := yaml.Unmarshal(data, &cf); err != nil {
		return nil, fmt.Errorf("parsing compose file: %w", err)
	}

	services := make(map[string]InfraService)
	for name, svc := range cf.Services {
		infra := InfraService{
			Name:  name,
			Image: svc.Image,
		}

		// Parse the first port mapping to extract env var and base port
		if len(svc.Ports) > 0 {
			envVar, port, err := parsePortMapping(svc.Ports[0])
			if err != nil {
				return nil, fmt.Errorf("service '%s': %w", name, err)
			}
			infra.Port = port
			infra.EnvVar = envVar
		}

		services[name] = infra
	}

	return services, nil
}

// parsePortMapping parses a docker compose port mapping string.
// Supported formats:
//   - "${REDIS_PORT:-6379}:6379" -> env var: REDIS_PORT, port: 6379
//   - "6379:6379"               -> env var: "", port: 6379
func parsePortMapping(mapping string) (envVar string, port int, err error) {
	// Try env var pattern: ${VAR:-default}:container_port
	if m := envVarPattern.FindStringSubmatch(mapping); m != nil {
		p, _ := strconv.Atoi(m[2])
		return m[1], p, nil
	}

	// Try static pattern: host_port:container_port
	if m := staticPortPattern.FindStringSubmatch(mapping); m != nil {
		p, _ := strconv.Atoi(m[1])
		return "", p, nil
	}

	return "", 0, fmt.Errorf("unsupported port mapping format: %s", mapping)
}
