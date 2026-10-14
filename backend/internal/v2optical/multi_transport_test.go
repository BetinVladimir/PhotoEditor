package v2optical

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
)

func TestMultiTransportPublishesFrozenInventoryAndRejectsAliases(t *testing.T) {
	for _, operation := range []string{"optical.panorama", "optical.focus_stack"} {
		t.Run(operation, func(t *testing.T) {
			pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
			qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			capacity, err := MultiResources(operation, params, originals)
			if err != nil {
				t.Fatal(err)
			}
			plan, err := CompileMultiTaskPlan(context.Background(), operation, params, originals, qualified, capacity)
			if err != nil {
				t.Fatal(err)
			}
			published := v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.ParamsSHA}
			publisher := &negativePublisher{}
			graph, err := BuildMultiTaskTransport(context.Background(), uuid.New(), plan, originals, qualified, published, publisher)
			if err != nil {
				t.Fatal(err)
			}
			if publisher.calls != 2 || graph.Built.Count != 7 || len(graph.Built.Chunks) != 1 || len(graph.Direct) != 5 {
				t.Fatal("incomplete transport")
			}
			plan.PhysicalImageInputs[0].Contract = "mutated"
			originals[0][0] = ' '
			cursor, err := graph.Open(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			count := 0
			for {
				entry, ok, err := cursor.Next(context.Background())
				if err != nil {
					t.Fatal(err)
				}
				if !ok {
					break
				}
				if entry.Contract == "mutated" {
					t.Fatal("cursor aliases caller plan")
				}
				count++
			}
			if count != 7 {
				t.Fatal("cursor lost frozen entries")
			}
		})
	}
}

func TestMultiTransportInvalidPlanDoesNotPublish(t *testing.T) {
	operation := "optical.panorama"
	pin, locks, report, calibration, params, originals := multiQualifiedFixture(t, operation)
	qualified, err := VerifyMultiQualificationEvidence(operation, pin, locks, report, calibration, params, originals)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := MultiResources(operation, params, originals)
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompileMultiTaskPlan(context.Background(), operation, params, originals, qualified, capacity)
	if err != nil {
		t.Fatal(err)
	}
	published := v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.ParamsSHA}
	publisher := &negativePublisher{alias: published.ArtifactID}
	if _, err := BuildMultiTaskTransport(context.Background(), uuid.New(), plan, originals, qualified, published, publisher); err == nil {
		t.Fatal("accepted transport/input alias")
	}
	publisher = &negativePublisher{}
	plan.ReservedBytes++
	if _, err := BuildMultiTaskTransport(context.Background(), uuid.New(), plan, originals, qualified, published, publisher); err == nil || publisher.calls != 0 {
		t.Fatal("published mutated plan")
	}
}
