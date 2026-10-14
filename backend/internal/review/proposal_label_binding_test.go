package review

import (
 "testing"
 "strings"
 "github.com/google/uuid"
)

func TestVerifiedProposalLabelBindingOwnsImmutablePins(t *testing.T) {
 p:=VerifiedProposal{ref:ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)},document:proposalDocument{
  LabelMap:ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("b",64)},Canvas:Canvas{Width:8,Height:9}}}
 binding,err:=p.LabelBinding();if err!=nil {t.Fatal(err)}
 if binding.Proposal!=p.ref || binding.Labels!=p.document.LabelMap || binding.Width!=8 || binding.Height!=9 {t.Fatal("changed label pins")}
 binding.Labels.SHA256=strings.Repeat("c",64)
 after,err:=p.LabelBinding();if err!=nil || after.Labels!=p.document.LabelMap {t.Fatal("mutable proposal pins",err)}
 if _,err:=(VerifiedProposal{}).LabelBinding();err==nil {t.Fatal("unverified zero proposal accepted")}
}
