package provenance

import (
 "context"
 "encoding/json"
 "errors"
 "os"
 "testing"

 "github.com/google/uuid"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2optical"
 "photoeditor/backend/internal/v2qualification"
)

func TestDelegatedNegativePolicyContentRequiresProvisionedPolicy(t *testing.T) {
 raw, err := os.ReadFile("../v2optical/testdata/negative-qualification.json")
 if err != nil { t.Fatal(err) }
 var fixture struct { Pin v2optical.NegativeQualificationPin; Locks v2qualification.DeploymentLocks }
 if json.Unmarshal(raw, &fixture) != nil { t.Fatal("invalid fixture") }
 historical := delegatedNegativeQualificationFunc(func(context.Context, CanonicalSigningTask) (v2optical.NegativeQualificationPin, error) {
  return fixture.Pin, nil
 })
 task := CanonicalSigningTask{Task: uuid.New(), Operation: "optical.negative", Version: "1"}
 reader := BoundDelegatedNegativeQualificationContent{Historical: historical}
 if _, err := reader.Read(context.Background(), task); err == nil { t.Fatal("missing policy accepted") }
 policies, err := NewSigningNegativePolicyRegistry([]InstalledSigningNegativePolicy{{
  PolicyID: fixture.Pin.PolicyID, PolicySHA: fixture.Pin.PolicySHA, Locks: fixture.Locks,
 }})
 if err != nil { t.Fatal(err) }
 reader.Policies = policies
 reader.Documents = delegatedDocumentsFunc(func(context.Context, CanonicalSigningTask,
  v2input.Ref, int) ([]byte, error) { return nil, ErrInvalid })
 ctx, cancel := context.WithCancel(context.Background()); cancel()
 if _, err := reader.Read(ctx, task); !errors.Is(err, context.Canceled) { t.Fatal("cancellation ignored", err) }
}
