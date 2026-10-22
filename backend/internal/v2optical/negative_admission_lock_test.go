package v2optical

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"

	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

func TestNegativeAdmissionRejectsInvalidPlanBeforeAnyRelationalLock(t *testing.T) {
	params, source, qualified, capacity := negativeDraftFixture(t)
	body, _, err := params.Compile()
	if err != nil { t.Fatal(err) }
	plan, err := CompileNegativeTaskPlan(context.Background(), params.Base, body, source, qualified, capacity)
	if err != nil { t.Fatal(err) }
	located, err := BindNegativeQualificationObjects(qualified, "photos",
		objectstore.Object{SHA256: qualified.pin.Report.SHA256, Bytes: int64(len(qualified.reportRaw)), ContentType: "application/json", Key: "report", VersionID: "r1"},
		objectstore.Object{SHA256: qualified.pin.Calibration.SHA256, Bytes: int64(len(qualified.calibrationRaw)), ContentType: "application/json", Key: "calibration", VersionID: "c1"})
	if err != nil { t.Fatal(err) }
	for _, mode := range []string{"changed_reserve", "params_alias", "missing_actor", "cancelled", "stale_source", "child_stale", "qualification_stale", "ok"} {
		t.Run(mode, func(t *testing.T) {
			who := auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}
			tenant, project, snapshot := uuid.New(), uuid.New(), uuid.New()
			tx := &negativeSourceScopeTx{negativeLockTx: negativeLockTx{tenant: tenant, mode: mode}}
			if mode == "stale_source" { tx.mode = "stale" }
			candidate := plan
			published := v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.ParamsSHA}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			switch mode {
			case "changed_reserve": candidate.ReservedBytes++
			case "params_alias": published.ArtifactID = params.Base.Source.ArtifactID
			case "missing_actor": who.UserID = uuid.Nil
			case "cancelled": cancel()
			}
			entries, direct, err := LockNegativeAdmission(ctx, tx, who, tenant, project, snapshot, candidate, source, located, published)
			if mode == "ok" {
				if err != nil || len(entries) != 6 || len(direct) != 4 { t.Fatal(entries, direct, err) }
				calls := strings.Join(tx.calls, "\n")
				if strings.Index(calls, "negative_source_child") > strings.Index(calls, "negative_qualification(") { t.Fatal("qualification/source lock order", calls) }
				if !strings.Contains(calls, "negative_qualification_objects") { t.Fatal("exact object locator gate omitted", calls) }
				return
			}
			if err == nil || entries != nil || direct != nil { t.Fatal("invalid/stale admission exposed inventory", err) }
			if mode == "changed_reserve" || mode == "params_alias" || mode == "missing_actor" || mode == "cancelled" {
				if len(tx.calls) != 0 { t.Fatal("invalid local proof reached SQL", tx.calls) }
			}
		})
	}
}
