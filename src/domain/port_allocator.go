package domain

import "hash/fnv"

const (
	// maxOffset is the upper bound of the port offset range (inclusive).
	maxOffset = 200
)

// AllocatePortOffset computes a deterministic port offset from an environment name
// using FNV-1a hashing. The offset is in the range [1, maxOffset].
func AllocatePortOffset(envName string) int {
	h := fnv.New32a()
	h.Write([]byte(envName))
	return int(h.Sum32()%uint32(maxOffset)) + 1
}

// AllocatePorts applies a deterministic offset to all base ports for the given
// environment name.
func AllocatePorts(envName string, basePorts map[string]int) PortMap {
	offset := AllocatePortOffset(envName)
	ports := make(PortMap, len(basePorts))
	for name, base := range basePorts {
		ports[name] = base + offset
	}
	return ports
}
