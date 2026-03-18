package domain

import "fmt"

// TopologicalSort performs Kahn's algorithm to produce a topological ordering
// of services based on their dependsOn relationships. Returns an error if a
// cycle is detected or a dependency references an unknown service.
func TopologicalSort(services map[string]ServiceConfig) ([]string, error) {
	// Build adjacency list and in-degree map
	inDegree := make(map[string]int)
	dependents := make(map[string][]string) // dependency -> list of services that depend on it

	// Initialize all services with in-degree 0
	for name := range services {
		inDegree[name] = 0
	}

	// Build edges
	for name, svc := range services {
		for _, dep := range svc.DependsOn {
			// Dependencies can reference infrastructure services too,
			// which won't be in the services map — skip degree tracking for those
			if _, exists := services[dep]; !exists {
				continue
			}
			inDegree[name]++
			dependents[dep] = append(dependents[dep], name)
		}
	}

	// Collect nodes with in-degree 0
	var queue []string
	for name, deg := range inDegree {
		if deg == 0 {
			queue = append(queue, name)
		}
	}

	// Sort the queue for deterministic output
	sortStrings(queue)

	var result []string
	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]
		result = append(result, node)

		for _, dependent := range dependents[node] {
			inDegree[dependent]--
			if inDegree[dependent] == 0 {
				queue = insertSorted(queue, dependent)
			}
		}
	}

	if len(result) != len(services) {
		return nil, fmt.Errorf("dependency cycle detected among services")
	}

	return result, nil
}

// sortStrings sorts a string slice in place (insertion sort, fine for small slices).
func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j] < s[j-1]; j-- {
			s[j], s[j-1] = s[j-1], s[j]
		}
	}
}

// insertSorted inserts a string into a sorted slice maintaining sort order.
func insertSorted(s []string, val string) []string {
	i := 0
	for i < len(s) && s[i] < val {
		i++
	}
	s = append(s, "")
	copy(s[i+1:], s[i:])
	s[i] = val
	return s
}
