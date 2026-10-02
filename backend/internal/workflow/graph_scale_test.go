package workflow

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func TestGraphOrderWideFanoutDeterministic(t *testing.T) {
	r, e := LoadRegistry()
	if e != nil {
		t.Fatal(e)
	}
	g := Graph{Nodes: []Node{{Key: "root", Operation: "asset.verify"}, {Key: "join", Operation: "asset.inspect"}}}
	for i := 0; i < 10000; i++ {
		key := fmt.Sprintf("leaf/%05d", i)
		g.Nodes = append(g.Nodes, Node{Key: key, Operation: "asset.decode"})
		g.Edges = append(g.Edges, Edge{"root", key, "success"}, Edge{key, "join", "success"})
	}
	order, e := g.Order(r)
	if e != nil || len(order) != 10002 || order[0].Key != "root" || order[len(order)-1].Key != "join" {
		t.Fatal("invalid fanout order", e)
	}
	for i := 0; i < 10000; i++ {
		if order[i+1].Key != fmt.Sprintf("leaf/%05d", i) {
			t.Fatal("unstable sibling order")
		}
	}
	random := rand.New(rand.NewSource(17))
	random.Shuffle(len(g.Nodes), func(i, j int) { g.Nodes[i], g.Nodes[j] = g.Nodes[j], g.Nodes[i] })
	random.Shuffle(len(g.Edges), func(i, j int) { g.Edges[i], g.Edges[j] = g.Edges[j], g.Edges[i] })
	again, e := g.Order(r)
	if e != nil || !reflect.DeepEqual(order, again) {
		t.Fatal("input order changed graph order", e)
	}
}
func BenchmarkGraphOrderWide(b *testing.B) {
	r, _ := LoadRegistry()
	g := Graph{}
	for i := 0; i < 10000; i++ {
		g.Nodes = append(g.Nodes, Node{Key: fmt.Sprintf("node/%05d", i), Operation: "asset.decode"})
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := g.Order(r); err != nil {
			b.Fatal(err)
		}
	}
}
