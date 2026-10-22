package workflow

import "testing"

func TestRegistryAdmitsSeparateRetouchPreviewAndShownCopyNodes(t *testing.T) {
	registry, err := LoadRegistry()
	if err != nil { t.Fatal(err) }
	graph := Graph{Nodes: []Node{
		{Key: "shown", Operation: "semantic.adopt_shown_retouch_region"},
		{Key: "preview", Operation: "semantic.preview_retouch_region"},
	}, Edges: []Edge{{From: "preview", To: "shown", Condition: "accepted"}}}
	nodes, err := graph.Order(registry)
	if err != nil || len(nodes) != 2 || nodes[0].Key != "preview" || nodes[1].Key != "shown" {
		t.Fatal("retouch operations unavailable to graph compilation", err)
	}
	for _, node := range nodes {
		op := registry[node.Operation]
		if op.ResourceClass != "cpu" || op.Model != nil || len(op.Backends) != 1 || op.Backends[0] != "cpu" {
			t.Fatal("CPU retouch acquired a GPU/model dependency", op)
		}
	}
	if registry[nodes[0].Operation].RoutingKey == registry[nodes[1].Operation].RoutingKey {
		t.Fatal("shown copy shares preview execution route")
	}
}
