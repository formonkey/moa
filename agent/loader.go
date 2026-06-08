package agent

import "fmt"

// Loader allows loading agents by name and getting the root agent.
type Loader interface {
	// ListAgents returns names of all available agents.
	ListAgents() []string
	// LoadAgent returns an agent by name. Returns error if not found.
	LoadAgent(name string) (Agent, error)
	// RootAgent returns the root agent.
	RootAgent() Agent
}

// NewSingleLoader returns a loader with one agent as the root.
func NewSingleLoader(a Agent) Loader {
	return &singleLoader{root: a}
}

// NewMultiLoader returns a loader with a root agent and additional agents.
// Returns error if any agents share the same name.
func NewMultiLoader(root Agent, agents ...Agent) (Loader, error) {
	m := make(map[string]Agent)
	m[root.Name()] = root
	for _, a := range agents {
		if _, ok := m[a.Name()]; ok {
			return nil, fmt.Errorf("duplicate agent name: %s", a.Name())
		}
		m[a.Name()] = a
	}
	return &multiLoader{agentMap: m, root: root}, nil
}

// --- singleLoader ---

type singleLoader struct {
	root Agent
}

func (s *singleLoader) ListAgents() []string { return []string{s.root.Name()} }
func (s *singleLoader) RootAgent() Agent     { return s.root }

func (s *singleLoader) LoadAgent(name string) (Agent, error) {
	if name == "" || name == s.root.Name() {
		return s.root, nil
	}
	return nil, fmt.Errorf("cannot load agent %q — use %q or empty string", name, s.root.Name())
}

// --- multiLoader ---

type multiLoader struct {
	agentMap map[string]Agent
	root     Agent
}

func (m *multiLoader) RootAgent() Agent { return m.root }

func (m *multiLoader) ListAgents() []string {
	agents := make([]string, 0, len(m.agentMap))
	for name := range m.agentMap {
		agents = append(agents, name)
	}
	return agents
}

func (m *multiLoader) LoadAgent(name string) (Agent, error) {
	a, ok := m.agentMap[name]
	if !ok {
		return nil, fmt.Errorf("agent %q not found, available: %v", name, m.ListAgents())
	}
	return a, nil
}
