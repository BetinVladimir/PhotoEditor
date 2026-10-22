package main

import (
	"github.com/google/uuid"
	"testing"
)

func TestOneOwnStopsEvenWaitingResource(t *testing.T) {
	s := fixture(t)
	var own uuid.UUID
	for id := range s.tasks {
		own = uuid.MustParse(id)
		break
	}
	for _, changed := range []bool{false, true} {
		calls := 0
		selected := map[string]bool{}
		n, e := dispatchOneOwn(s, selected, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
			calls++
			if calls > 1 {
				t.Fatal("selected second own")
			}
			return true, changed, accept(own, "media", []byte(`{}`))
		})
		if e != nil || calls != 1 || len(selected) != 1 || n != map[bool]int{false: 0, true: 1}[changed] {
			t.Fatal(n, e, calls)
		}
	}
}
func TestSixNotDueOwnFirstNeverSelectsSecond(t *testing.T) {
	s := expandedFixture(t)
	var own uuid.UUID
	for id := range s.tasks {
		own = uuid.MustParse(id)
		break
	}
	calls := 0
	_, e := dispatchOneOwn(s, map[string]bool{}, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
		calls++
		if calls > 1 {
			t.Fatal("second pick after own when six not due")
		}
		return true, true, accept(own, "media", []byte(`{}`))
	})
	if e != nil || calls != 1 {
		t.Fatal(e, calls)
	}
}
func TestSixDueThenExactlyOneOwn(t *testing.T) {
	s := expandedFixture(t)
	ids := []uuid.UUID{}
	for id := range s.deferred {
		ids = append(ids, uuid.MustParse(id))
	}
	for id := range s.tasks {
		ids = append(ids, uuid.MustParse(id))
		break
	}
	calls := 0
	selected := map[string]bool{}
	n, e := dispatchOneOwn(s, selected, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
		if calls >= len(ids) {
			t.Fatal("second own selected")
		}
		id := ids[calls]
		calls++
		return true, true, accept(id, "media", []byte(`{}`))
	})
	if e != nil || calls != 7 || n != 7 || len(selected) != 7 {
		t.Fatal(e, calls, n)
	}
}
func TestForeignAfterDefersRefusesWholeCaller(t *testing.T) {
	s := expandedFixture(t)
	_, e := dispatchOneOwn(s, map[string]bool{}, func(accept func(uuid.UUID, string, []byte) error) (bool, bool, error) {
		return true, true, accept(uuid.New(), "media", []byte(`{}`))
	})
	if e == nil {
		t.Fatal("foreign accepted")
	}
}
