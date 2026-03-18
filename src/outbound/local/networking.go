package local

import (
	"fmt"

	"github.com/jake-landersweb/previewctl/src/domain"
)

// NetworkingAdapter implements domain.NetworkingPort for local development.
// It uses deterministic FNV-1a port allocation from the domain layer.
type NetworkingAdapter struct {
	basePorts map[string]int
}

// NewNetworkingAdapter creates a new local networking adapter.
func NewNetworkingAdapter(config *domain.ProjectConfig) *NetworkingAdapter {
	return &NetworkingAdapter{
		basePorts: config.AllBasePorts(),
	}
}

func (a *NetworkingAdapter) AllocatePorts(envName string) domain.PortMap {
	return domain.AllocatePorts(envName, a.basePorts)
}

func (a *NetworkingAdapter) GetServiceURL(envName string, service string) (string, error) {
	ports := a.AllocatePorts(envName)
	port, ok := ports[service]
	if !ok {
		return "", fmt.Errorf("unknown service '%s'", service)
	}
	return fmt.Sprintf("http://localhost:%d", port), nil
}
