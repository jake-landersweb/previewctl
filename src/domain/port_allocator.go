package domain

import (
	"fmt"
	"hash/fnv"
	"net"
	"sort"
)

const (
	portRangeStart = 61000
	portRangeEnd   = 65000
	portsPerEnv    = 100
)

// maxEnvironments is the number of environment blocks that fit in the range.
var maxEnvironments = (portRangeEnd - portRangeStart) / portsPerEnv

// AllocatePortBlock selects a block of ports for an environment and assigns
// one port per service name. Ports are checked for availability.
// Returns a PortMap with service names mapped to allocated ports.
func AllocatePortBlock(envName string, serviceNames []string) (PortMap, error) {
	blockStart := pickBlockStart(envName)

	ports := make(PortMap, len(serviceNames))

	// Sort service names for deterministic assignment
	sorted := make([]string, len(serviceNames))
	copy(sorted, serviceNames)
	sort.Strings(sorted)

	offset := 0
	for _, name := range sorted {
		port := blockStart + offset
		if port >= blockStart+portsPerEnv {
			return nil, fmt.Errorf("too many services (%d) for port block size (%d)", len(serviceNames), portsPerEnv)
		}

		// Find a free port within the block
		for !isPortFree(port) {
			offset++
			port = blockStart + offset
			if port >= blockStart+portsPerEnv {
				return nil, fmt.Errorf("could not find free ports in block %d-%d", blockStart, blockStart+portsPerEnv-1)
			}
		}

		ports[name] = port
		offset++
	}

	return ports, nil
}

// pickBlockStart deterministically selects a block start from the environment name.
func pickBlockStart(envName string) int {
	h := fnv.New32a()
	h.Write([]byte(envName))
	blockIndex := int(h.Sum32()) % maxEnvironments
	return portRangeStart + (blockIndex * portsPerEnv)
}

// isPortFree checks if a TCP port is available by attempting to listen on it.
func isPortFree(port int) bool {
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		return false
	}
	_ = ln.Close()
	return true
}

// ServiceNames returns a sorted list of all service and infrastructure names
// that need port assignments.
func (c *ProjectConfig) ServiceNames() []string {
	names := make([]string, 0, len(c.Services)+len(c.InfraServices)+len(c.Core.Databases))
	for name := range c.Services {
		names = append(names, name)
	}
	for name := range c.InfraServices {
		names = append(names, name)
	}
	for name, db := range c.Core.Databases {
		if db.Local != nil && db.Local.Port > 0 {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	return names
}
