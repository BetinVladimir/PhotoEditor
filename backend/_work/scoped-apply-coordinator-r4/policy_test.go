package main

import (
	"encoding/json"
	"github.com/google/uuid"
	"strings"
	"testing"
	"time"
)

func syntheticScope() scope {
	return scope{Schema: 2, ObservedAt: time.Date(2026, 10, 22, 8, 0, 0, 0, time.UTC), Head: 2654, LocatorInstallSHA: strings.Repeat("4", 64), HeadInstallSHA: strings.Repeat("1", 64), GatewayReadySHA: strings.Repeat("2", 64), PreviewDeliverySHA: strings.Repeat("3", 64), Tenant: uuid.MustParse("5dbe7f17-b560-4696-9525-03959e843841"), Project: project, Collection: collection, Items: append([]itemBinding(nil), exactItems...), Style: fixedStyle, StyleSHA: fixedStyleSHA, Run: uuid.MustParse("11111111-1111-4111-8111-111111111111"), Output: uuid.MustParse("22222222-2222-4222-8222-222222222222"), ConfigSHA: strings.Repeat("a", 64), CPUReceiptSHA: strings.Repeat("b", 64), HumanReviewSHA: strings.Repeat("c", 64), APIReceiptSHA: strings.Repeat("d", 64), SourceManifestSHA: strings.Repeat("e", 64), ReadbackSHA: strings.Repeat("f", 64), Phase: "resolve", Graph: 4}
}
func TestExactThreeSyntheticPolicy(t *testing.T) {
	s := syntheticScope()
	if e := s.validate(s.ObservedAt.Add(time.Second)); e != nil {
		t.Fatal(e)
	}
}
func TestClosedScopeSubstitutions(t *testing.T) {
	for _, change := range []func(*scope){func(s *scope) { s.Schema = 1 }, func(s *scope) { s.Head = 2653 }, func(s *scope) { s.LocatorInstallSHA = "" }, func(s *scope) { s.Head = 2649 }, func(s *scope) { s.Head = 2650 }, func(s *scope) { s.HeadInstallSHA = "" }, func(s *scope) { s.GatewayReadySHA = "" }, func(s *scope) { s.PreviewDeliverySHA = "" }, func(s *scope) { s.Items[0].MasterSHA = strings.Repeat("f", 64) }, func(s *scope) { s.Items[1] = s.Items[0] }, func(s *scope) { s.Style = uuid.Nil }, func(s *scope) { s.HumanReviewSHA = "" }, func(s *scope) { s.Graph = 3 }, func(s *scope) {
		s.Tasks = []taskBinding{{ID: s.Run, Class: "gpu", Operation: "render.compose", ProfileSHA: strings.Repeat("a", 64)}}
	}} {
		s := syntheticScope()
		change(&s)
		if s.validate(s.ObservedAt) == nil {
			t.Fatal("substitution accepted")
		}
	}
}
func TestUnknownRowsRemainProtected(t *testing.T) {
	s := syntheticScope()
	skip, e := s.normalize("artifacts", map[string]json.RawMessage{"tenant_id": json.RawMessage(`"5dbe7f17-b560-4696-9525-03959e843841"`)})
	if e != nil || skip {
		t.Fatal("blanket tenant exemption")
	}
}
