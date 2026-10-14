package review

import (
 "strings"
 "testing"

 "github.com/google/uuid"
)

func TestNegativePersistenceRequiresExactPrivateBinding(t *testing.T){
 pin:=func()ArtifactRef{return ArtifactRef{ID:uuid.New(),SHA256:strings.Repeat("a",64)}}
 snapshot:=CandidateSnapshot{Candidate:pin(),Base:pin()}
 original:=negativeCandidateBinding{params:pin(),base:snapshot.Base,candidate:snapshot.Candidate}
 if !validNegativeCandidateBinding(&original,snapshot){t.Fatal("valid binding refused")}
 if validNegativeCandidateBinding(nil,snapshot){t.Fatal("missing physical binding accepted")}
 for _,mode:=range []string{"params","base","base_sha","candidate","candidate_sha"}{
  changed:=original
  switch mode{case "params":changed.params.ID=uuid.Nil;case "base":changed.base.ID=uuid.New();case "base_sha":changed.base.SHA256="invalid";case "candidate":changed.candidate.ID=uuid.New();case "candidate_sha":changed.candidate.SHA256=strings.Repeat("b",64)}
  if validNegativeCandidateBinding(&changed,snapshot){t.Fatal("mismatched private binding accepted",mode)}
 }
}
