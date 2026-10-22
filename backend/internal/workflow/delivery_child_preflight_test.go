package workflow

import (
 "context"
 "testing"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
 "photoeditor/backend/internal/artifactjson"
 "photoeditor/backend/internal/delivery"
 "photoeditor/backend/internal/domain"
 "photoeditor/backend/internal/exportpreset"
)

// Embedded nil Objects deliberately panics if publication begins. The pool is
// also deliberately uninitialized: a budget refusal must precede all SQL/PUT.
type deliveryPreflightNoObjects struct { artifactjson.Objects }
type deliveryPreflightResolver struct { source DeliveryResolvedChild }
func (r deliveryPreflightResolver) ResolveChild(context.Context, delivery.ExpansionLease, delivery.PreparePlan, delivery.ExportChildIntent) (DeliveryResolvedChild, error) {
 return r.source, nil
}
func TestDeliveryChildImpossibleBudgetRefusedBeforeParamsIO(t *testing.T) {
 for _, resize := range []bool{false, true} {
  plan, parent, child, raw, base := deliveryExportPlanFixture(t, func(config *exportpreset.Config) {
   if resize { edge := 200; config.Size = &exportpreset.Size{LongEdge: &edge, Fit: "contain", AllowUpscale: false} }
  })
  registry, err := LoadRegistry()
  if err != nil { t.Fatal(err) }
  lease := delivery.ExpansionLease{Run: parent, ParentTask: plan.TaskID(), Expansion: uuid.New(), Token: uuid.New(), SlotCount: 1}
  pipeline := DeliveryChildPipeline{
   DB: &pgxpool.Pool{}, Objects: deliveryPreflightNoObjects{}, Bucket: "photo",
   Resolver: deliveryPreflightResolver{source: DeliveryResolvedChild{ImageJCS: raw, TargetICCSHA: base.TargetICCSHA256}},
   Registry: registry, Limits: DeliveryDerivativeLimits{RAMBytes: 1, ScratchBytes: 1, OutputBytes: 1},
  }
  result, err := pipeline.ProcessChild(context.Background(), lease, plan, child)
  if err == nil || domain.AsError(err).Code != "DELIVERY_RESOURCE_INVALID" || result.RunID != uuid.Nil {
   t.Fatal("impossible budget did not fail before publication", resize, result, err)
  }
 }
}
