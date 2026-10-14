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

func TestDelegatedNegativePixelAncestryRequiresStableExactSource(t *testing.T) {
 for _, mode := range []string{"valid", "self", "missing", "changed", "error", "cancelled", "cancel_during_read", "wrong_task", "bad_source"} {
  t.Run(mode, func(t *testing.T) {
   task := CanonicalSigningTask{Task: uuid.New(), Operation: "optical.negative", Version: "1"}
   sha, err := canonical.Hash(task); if err != nil { t.Fatal(err) }
   source := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
   evidence := SigningNegativeEvidence{taskDigest: sha, pixels: SigningNegativePixelEvidence{taskDigest: sha}}
   evidence.pixels.params.Base.Source = source
   if mode == "bad_source" { evidence.pixels.params.Base.Source.SHA256 = "bad" }
   if mode == "wrong_task" { task.Task = uuid.New() }
   ctx, cancel := context.WithCancel(context.Background()); defer cancel()
   if mode == "cancelled" { cancel() }
   producer := uuid.New(); calls := 0; failure := errors.New("producer unavailable")
   resolver := delegatedImageProducerFunc(func(_ context.Context, input v2input.Ref) (uuid.UUID, error) {
    calls++
    if input != source { t.Fatal("wrong source resolved") }
    if mode == "cancel_during_read" { cancel() }
    switch mode {
    case "self": return task.Task, nil
    case "missing": return uuid.Nil, nil
    case "changed": if calls > 1 { return uuid.New(), nil }
    case "error": return uuid.Nil, failure
    }
    return producer, nil
   })
   got, err := ResolveDelegatedNegativePixelAncestry(ctx, resolver, task, evidence)
   if mode == "valid" {
    if err != nil || calls != 2 || got.input != source || got.producer != producer { t.Fatal("valid edge refused", err) }
    return
   }
   if err == nil || got.input.ArtifactID != uuid.Nil { t.Fatal("invalid edge escaped", mode, err) }
   if mode == "error" && !errors.Is(err, failure) { t.Fatal("authority error hidden", err) }
   if (mode == "cancelled" || mode == "cancel_during_read") && !errors.Is(err, context.Canceled) { t.Fatal("cancellation ignored", err) }
   if (mode == "cancelled" || mode == "wrong_task" || mode == "bad_source") && calls != 0 { t.Fatal("invalid preflight performed IO", calls) }
   if mode == "cancel_during_read" && calls != 1 { t.Fatal("cancelled lookup repeated", calls) }
  })
 }
}

func TestDelegatedNegativeCausalSourceRequiresConfiguredAuthorities(t *testing.T) {
 if _, err := (BoundDelegatedNegativeCausalSource{}).ReadDelegatedNegativeCausal(
  context.Background(), ReadySigningExport{}, CanonicalSigningTask{}); err == nil {
  t.Fatal("unconfigured delegated causal source accepted")
 }
}
