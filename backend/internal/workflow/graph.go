package workflow

import (
	"container/heap"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

//go:embed operations.json
var registryFS embed.FS

type Operation struct {
	Name           string   `json:"name"`
	Version        string   `json:"version"`
	ResourceClass  string   `json:"resource_class"`
	RoutingKey     string   `json:"routing_key"`
	Model          *string  `json:"model"`
	TimeoutSeconds int      `json:"timeout_seconds"`
	Backends       []string `json:"backends"`
}
type Registry map[string]Operation

func LoadRegistry() (Registry, error) {
	b, err := registryFS.ReadFile("operations.json")
	if err != nil {
		return nil, err
	}
	var doc struct {
		Operations []Operation `json:"operations"`
	}
	if err = json.Unmarshal(b, &doc); err != nil {
		return nil, err
	}
	r := Registry{}
	for _, op := range doc.Operations {
		if _, exists := r[op.Name]; exists || op.Name == "" || op.Version != "1" || op.TimeoutSeconds < 1 || op.TimeoutSeconds > 7200 {
			return nil, errors.New("invalid operation registry")
		}
		r[op.Name] = op
	}
	return r, nil
}

type Node struct {
	Key, Operation  string
	Disabled        bool
	PassThroughFrom string
}
type Edge struct{ From, To, Condition string }
type Graph struct {
	Nodes []Node
	Edges []Edge
}

// Order is deterministic independent of input ordering and rejects accidental
// duplicated edges as well as cycles before anything is persisted or dispatched.
func (g Graph) Order(registry Registry) ([]Node, error) {
	nodes := map[string]Node{}
	indegree := map[string]int{}
	children := map[string][]string{}
	for _, n := range g.Nodes {
		if n.Key == "" || len(n.Key) > 240 || strings.ContainsAny(n.Key, "\x00\r\n") {
			return nil, errors.New("invalid node key")
		}
		if _, ok := nodes[n.Key]; ok {
			return nil, errors.New("duplicate node")
		}
		if _, ok := registry[n.Operation]; !ok {
			return nil, fmt.Errorf("unknown operation %s", n.Operation)
		}
		if n.Disabled && n.PassThroughFrom == "" {
			return nil, errors.New("disabled node lacks pass-through")
		}
		nodes[n.Key] = n
		indegree[n.Key] = 0
	}
	seen := map[string]bool{}
	for _, e := range g.Edges {
		_, from := nodes[e.From]
		_, to := nodes[e.To]
		if !from || !to || e.From == e.To {
			return nil, errors.New("invalid edge endpoint")
		}
		if e.Condition != "success" && e.Condition != "accepted" && e.Condition != "rejected" && e.Condition != "terminal" {
			return nil, errors.New("invalid edge condition")
		}
		key := e.From + "\x00" + e.To
		if seen[key] {
			return nil, errors.New("duplicate edge")
		}
		seen[key] = true
		indegree[e.To]++
		children[e.From] = append(children[e.From], e.To)
	}
	for _, n := range nodes {
		if n.Disabled {
			if _, ok := nodes[n.PassThroughFrom]; !ok || !seen[n.PassThroughFrom+"\x00"+n.Key] {
				return nil, errors.New("pass-through must be direct dependency")
			}
		}
	}
	ready := nodeKeyHeap{}
	for key, n := range indegree {
		if n == 0 {
			ready = append(ready, key)
		}
	}
	heap.Init(&ready)
	out := make([]Node, 0, len(nodes))
	for len(ready) > 0 {
		key := heap.Pop(&ready).(string)
		out = append(out, nodes[key])
		for _, child := range children[key] {
			indegree[child]--
			if indegree[child] == 0 {
				heap.Push(&ready, child)
			}
		}
	}
	if len(out) != len(nodes) {
		return nil, errors.New("DAG contains cycle")
	}
	return out, nil
}

// Lexicographic heap preserves deterministic Kahn ordering in O(V log V + E).
type nodeKeyHeap []string

func (h nodeKeyHeap) Len() int           { return len(h) }
func (h nodeKeyHeap) Less(i, j int) bool { return h[i] < h[j] }
func (h nodeKeyHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *nodeKeyHeap) Push(v any)        { *h = append(*h, v.(string)) }
func (h *nodeKeyHeap) Pop() any {
	old := *h
	n := len(old)
	v := old[n-1]
	old[n-1] = ""
	*h = old[:n-1]
	return v
}
