package delivery

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestDeliverySummaryRejectsMalformedAndUnorderedRows(t *testing.T) {
	project := uuid.New()
	base := PackageSummary{ID: uuid.New(), ProjectID: project, WorkflowID: uuid.New(), Name: "Delivery", State: "building", Revision: "1", CreatedAt: time.Now().UTC()}
	if !validPackageSummary(base, project, nil, nil) {
		t.Fatal("valid summary refused")
	}
	for _, change := range []func(*PackageSummary){
		func(v *PackageSummary) { v.ID = uuid.Nil },
		func(v *PackageSummary) { v.ProjectID = uuid.New() },
		func(v *PackageSummary) { v.WorkflowID = uuid.Nil },
		func(v *PackageSummary) { v.Name = "" },
		func(v *PackageSummary) { v.Name = strings.Repeat("x", 201) },
		func(v *PackageSummary) { v.State = "unknown" },
		func(v *PackageSummary) { v.Revision = "01" },
		func(v *PackageSummary) { v.Revision = "9007199254740992" },
		func(v *PackageSummary) { v.CreatedAt = time.Time{} },
	} {
		row := base
		change(&row)
		if validPackageSummary(row, project, nil, nil) {
			t.Fatal("malformed summary accepted", row)
		}
	}
	before := base.CreatedAt.Add(-time.Second)
	priorID := uuid.New()
	if !validPackageSummary(base, project, &before, &priorID) {
		t.Fatal("next keyset row refused")
	}
	if validPackageSummary(base, project, &base.CreatedAt, &base.ID) {
		t.Fatal("duplicate keyset row accepted")
	}
	if validPackageSummary(base, project, &before, nil) {
		t.Fatal("half-bound keyset row accepted")
	}
	after := base.CreatedAt.Add(time.Second)
	if validPackageSummary(base, project, &after, &priorID) {
		t.Fatal("older keyset row accepted")
	}
}
