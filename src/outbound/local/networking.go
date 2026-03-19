package local

import (
	"fmt"

	"github.com/jake-landersweb/previewctl/src/domain"
)

// NetworkingAdapter implements domain.NetworkingPort for local development.
// It uses deterministic FNV-1a port block allocation from the domain layer.
type NetworkingAdapter struct {
	serviceNames []string
}

// NewNetworkingAdapter creates a new local networking adapter.
func NewNetworkingAdapter(config *domain.ProjectConfig) *NetworkingAdapter {
	return &NetworkingAdapter{
		serviceNames: config.ServiceNames(),
	}
}

func (a *NetworkingAdapter) AllocatePorts(envName string) (domain.PortMap, error) {
	return domain.AllocatePortBlock(envName, a.serviceNames)
}

func (a *NetworkingAdapter) GetServiceURL(envName string, service string) (string, error) {
	ports, err := a.AllocatePorts(envName)
	if err != nil {
		return "", fmt.Errorf("allocating ports: %w", err)
	}
	port, ok := ports[service]
	if !ok {
		return "", fmt.Errorf("unknown service '%s'", service)
	}
	return fmt.Sprintf("http://localhost:%d", port), nil
}
