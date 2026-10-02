package workflow

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"github.com/google/uuid"
	"testing"
)

func TestIngestPublishedReaderReconstructsOrderAndRejectsMissing(t *testing.T) {
	spec, r := ingestFixture(t)
	plan, _ := CompileIngest(spec, r)
	values := [][]any{}
	for i := len(plan.Nodes) - 1; i >= 0; i-- {
		node := plan.Nodes[i]
		sum := sha256.Sum256(node.Params)
		values = append(values, []any{node.ID, node.Key, node.Operation, uuid.New(), hex.EncodeToString(sum[:])})
	}
	rows := &outcomeRows{values: values}
	reader := IngestPublishedReader{DB: outcomeDB{rows}}
	docs, err := reader.Load(context.Background(), uuid.New(), spec)
	if err != nil || len(docs) != len(plan.Nodes) || !rows.closed {
		t.Fatal("resume read failed", err)
	}
	for i, d := range docs {
		if d.Node.ID != plan.Nodes[i].ID {
			t.Fatal("database row order changed task order")
		}
	}
	rows = &outcomeRows{values: values[:len(values)-1]}
	reader.DB = outcomeDB{rows}
	if _, err = reader.Load(context.Background(), uuid.New(), spec); err == nil || !rows.closed {
		t.Fatal("incomplete params accepted")
	}
	values[0][4] = "bad"
	rows = &outcomeRows{values: values}
	reader.DB = outcomeDB{rows}
	if _, err = reader.Load(context.Background(), uuid.New(), spec); err == nil || !rows.closed {
		t.Fatal("changed params accepted")
	}
}
