package provenance

import (
 "context"
 "errors"
 "strings"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"
)

func TestDelegatedRenderPixelAncestryRequiresStableProducers(t *testing.T) {
 for _, mode := range []string{"valid", "missing", "self", "changed", "duplicate", "task", "producer_error"} {
  t.Run(mode, func(t *testing.T) {
   pin := func() v2input.Ref { return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)} }
   task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1"}
   digest, err := canonical.Hash(task)
   if err != nil { t.Fatal(err) }
   inventory := SigningRenderCausalInventory{taskDigest: digest, Images: []v2input.Ref{pin(), pin()}, Results: []v2input.Ref{pin()}}
   if mode == "duplicate" { inventory.Results[0] = inventory.Images[0] }
   if mode == "task" { task.Task = uuid.New() }
   producer := uuid.New()
   failure := errors.New("producer unavailable")
   calls := 0
   source := delegatedImageProducerFunc(func(context.Context, v2input.Ref) (uuid.UUID, error) {
    calls++
    switch mode {
    case "missing": return uuid.Nil, nil
    case "self": return task.Task, nil
    case "producer_error": return uuid.Nil, failure
    case "changed": if calls > 3 { return uuid.New(), nil }
    }
    return producer, nil
   })
   got, err := ResolveDelegatedRenderPixelAncestry(context.Background(), source, task, inventory)
   if mode == "valid" {
    if err != nil || len(got.inputs) != 3 || len(got.parents) != 1 || got.parents[0] != producer || calls != 6 {
     t.Fatal("delegated ancestry lost", err, calls)
    }
    inventory.Images[0].SHA256 = "changed"
    if got.inputs[0].SHA256 == "changed" { t.Fatal("ancestry aliases inventory") }
    return
   }
   if err == nil || got.inputs != nil || got.parents != nil { t.Fatal("invalid ancestry escaped", mode, err) }
   if mode == "producer_error" && !errors.Is(err, failure) { t.Fatal("producer error hidden", err) }
   if (mode == "duplicate" || mode == "task") && calls != 0 { t.Fatal("invalid request reached producer", calls) }
  })
 }
}

func TestDelegatedRenderPixelAncestryStopsOnCancellation(t *testing.T) {
 task := CanonicalSigningTask{Task: uuid.New(), Operation: "render.compose", Version: "1"}
 digest, err := canonical.Hash(task)
 if err != nil { t.Fatal(err) }
 inventory := SigningRenderCausalInventory{taskDigest: digest, Images: []v2input.Ref{
  {ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)},
  {ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)},
 }}
 ctx, cancel := context.WithCancel(context.Background())
 defer cancel()
 calls := 0
 source := delegatedImageProducerFunc(func(context.Context, v2input.Ref) (uuid.UUID, error) {
  calls++
  cancel()
  return uuid.New(), nil
 })
 got, err := ResolveDelegatedRenderPixelAncestry(ctx, source, task, inventory)
 if !errors.Is(err, context.Canceled) || calls != 1 || got.inputs != nil {
  t.Fatal("cancelled producer read escaped", err, calls)
 }
}
