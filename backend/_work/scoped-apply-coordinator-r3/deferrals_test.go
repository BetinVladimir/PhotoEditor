package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"testing"
	"time"
)

func deferralFixture() scope {
	s := syntheticScope()
	s.deferred = map[string]deferBinding{}
	for i := 0; i < 6; i++ {
		id := uuid.UUID{15: byte(i + 1)}.String()
		s.deferred[id] = deferBinding{Task: id, Tenant: "0c43c2b6-58bb-4bd0-b184-83b8c51a108c", Run: uuid.UUID{15: 99}.String()}
	}
	raw := []byte(`{"backend":"cpu"}`)
	h := sha256.Sum256(raw)
	s.Tasks = []taskBinding{{ID: s.Run, Class: "cpu", Operation: "image.color", ProfileSHA: hex.EncodeToString(h[:])}}
	return s
}
func TestSixThenFirstOwnUnqueuedStops(t *testing.T) {
	s := deferralFixture()
	ids := []string{}
	for id := range s.deferred {
		ids = append(ids, id)
	}
	calls := 0
	selected := map[string]bool{}
	n, e := dispatchBounded(s, selected, func(check func(uuid.UUID, string, []byte) error) (bool, bool, error) {
		i := calls
		calls++
		if i < 6 {
			return true, false, check(uuid.MustParse(ids[i]), "media", []byte(`{}`))
		}
		if i == 6 {
			return true, false, check(s.Run, "cpu", []byte(`{"backend":"cpu"}`))
		}
		return false, false, errors.New("must stop after own waiting_resource")
	})
	if e != nil || n != 0 || calls != 7 || len(selected) != 7 {
		t.Fatalf("%v %d %d", e, n, calls)
	}
}
func TestUnknownAndDuplicateSelectionRefused(t *testing.T) {
	for _, duplicate := range []bool{false, true} {
		s := deferralFixture()
		var id uuid.UUID
		for k := range s.deferred {
			id = uuid.MustParse(k)
			break
		}
		calls := 0
		_, e := dispatchBounded(s, map[string]bool{}, func(check func(uuid.UUID, string, []byte) error) (bool, bool, error) {
			calls++
			if !duplicate {
				id = uuid.UUID{15: 88}
			}
			return true, false, check(id, "media", []byte(`{}`))
		})
		if e == nil {
			t.Fatal("unknown/duplicate accepted")
		}
	}
}
func TestDeferralTimestampAndNonselectedExact(t *testing.T) {
	now := time.Now().UTC()
	reason := "waiting_worker"
	later := now.Add(time.Second)
	a := []deferralRow{{Task: "a", Immutable: "hash"}}
	b := []deferralRow{{Task: "a", Immutable: "hash", When: &later, Reason: &reason}}
	if validateDeferrals(a, b, map[string]bool{"a": true}, now, now) != nil {
		t.Fatal("valid normal deferral refused")
	}
	if validateDeferrals(a, b, map[string]bool{}, now, now) == nil {
		t.Fatal("nonselected change accepted")
	}
	b[0].Immutable = "foreign"
	if validateDeferrals(a, b, map[string]bool{"a": true}, now, now) == nil {
		t.Fatal("whole hash drift accepted")
	}
}
func TestBlockedForeignRowWholeProtected(t *testing.T) {
	s := deferralFixture()
	row := map[string]json.RawMessage{"id": json.RawMessage(`"99999999-9999-4999-8999-999999999999"`), "state": json.RawMessage(`"blocked"`), "admission_after": json.RawMessage(`null`)}
	before, _ := json.Marshal(row)
	skip, e := s.normalize("tasks", row)
	after, _ := json.Marshal(row)
	if e != nil || skip || string(before) != string(after) {
		t.Fatal("blocked outside row relaxed")
	}
}
func TestExactReviewedSixSourceRequired(t *testing.T) {
	s := syntheticScope()
	if e := s.loadDeferrals(); e != nil || len(s.deferred) != 6 {
		t.Fatal(e)
	}
	if s.withDeferrals([]byte(`{"bindings":[]}`), sixDeferralSHA) == nil {
		t.Fatal("foreign source allowed")
	}
}
