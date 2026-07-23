package main

import "sync"

type Registry struct {
	mu     sync.RWMutex
	states []*NodeState
}

func NewRegistry(nodes []NodeEntry) *Registry {
	r := &Registry{states: make([]*NodeState, len(nodes))}
	for i, n := range nodes {
		r.states[i] = &NodeState{Entry: n, PingMs: -1}
	}
	return r
}

func (r *Registry) GetAll() []NodeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]NodeState, len(r.states))
	for i, s := range r.states {
		out[i] = *s
	}
	return out
}

func (r *Registry) GetByName(name string) *NodeState {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, s := range r.states {
		if s.Entry.Name == name {
			cp := *s
			return &cp
		}
	}
	return nil
}

func (r *Registry) Update(idx int, fn func(*NodeState)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if idx >= 0 && idx < len(r.states) {
		fn(r.states[idx])
	}
}

func (r *Registry) Add(entry NodeEntry) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.states = append(r.states, &NodeState{Entry: entry, PingMs: -1})
}

func (r *Registry) Remove(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	n := 0
	for _, s := range r.states {
		if s.Entry.Name != name {
			r.states[n] = s
			n++
		}
	}
	r.states = r.states[:n]
}

func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.states)
}
