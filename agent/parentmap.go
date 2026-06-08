package agent

import "fmt"

// ParentMap maps agent names to their parent agents.
// It is precomputed at runner initialization from the agent tree.
type ParentMap map[string]Agent

// BuildParentMap traverses the agent tree rooted at root and returns a
// map from each agent's name to its parent agent. The root agent has no parent.
// Returns an error if duplicate agent names are found in the tree.
func BuildParentMap(root Agent) (ParentMap, error) {
	m := make(ParentMap)
	if err := buildParentMapRecursive(root, nil, m); err != nil {
		return nil, err
	}
	return m, nil
}

func buildParentMapRecursive(ag, parent Agent, m ParentMap) error {
	if _, exists := m[ag.Name()]; exists && parent != nil {
		return fmt.Errorf("duplicate agent name %q in agent tree", ag.Name())
	}
	if parent != nil {
		m[ag.Name()] = parent
	}
	for _, sub := range ag.SubAgents() {
		if err := buildParentMapRecursive(sub, ag, m); err != nil {
			return err
		}
	}
	return nil
}
