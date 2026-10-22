package main

import (
	"encoding/json"
	"github.com/google/uuid"
	"testing"
)

func TestActiveThirteenBindingsAccepted(t *testing.T) {
	ids := []uuid.UUID{}
	tenants := map[uuid.UUID]uuid.UUID{}
	for i := 0; i < 13; i++ {
		id := uuid.New()
		ids = append(ids, id)
		tenants[id] = uuid.New()
	}
	a, _ := json.Marshal(ids)
	b, _ := json.Marshal(tenants)
	t.Setenv("EXPECTED_RUN_IDS", string(a))
	t.Setenv("EXPECTED_RUN_TENANTS", string(b))
	got, bindings, err := activeScopeFromEnvironment()
	if err != nil || len(got) != 13 || len(bindings) != 13 {
		t.Fatal("active subset denied", err)
	}
}
func TestBindingDriftRejectedBeforeDatabase(t *testing.T) {
	id := uuid.New()
	for _, test := range []struct {
		name     string
		ids      []uuid.UUID
		bindings map[uuid.UUID]uuid.UUID
	}{{"extra", []uuid.UUID{id}, map[uuid.UUID]uuid.UUID{id: uuid.New(), uuid.New(): uuid.New()}}, {"wrong_key", []uuid.UUID{id}, map[uuid.UUID]uuid.UUID{uuid.New(): uuid.New()}}, {"empty", nil, nil}, {"missing", []uuid.UUID{id}, nil}, {"zero_tenant", []uuid.UUID{id}, map[uuid.UUID]uuid.UUID{id: uuid.Nil}}} {
		t.Run(test.name, func(t *testing.T) {
			a, _ := json.Marshal(test.ids)
			b, _ := json.Marshal(test.bindings)
			t.Setenv("EXPECTED_RUN_IDS", string(a))
			t.Setenv("EXPECTED_RUN_TENANTS", string(b))
			if _, _, err := activeScopeFromEnvironment(); err == nil {
				t.Fatal("bad scope accepted")
			}
		})
	}
}
