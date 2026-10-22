package v2region

import "testing"

func TestAuthoredRegionNeverPublishesFromStatusAloneOrBeforeSettlement(t *testing.T) {
	cases := []struct {
		name  string
		facts authoredRegionFacts
		want  AuthoredRegionPhase
	}{
		{"admitted", authoredRegionFacts{Current: true}, AuthoredRegionAdmitted},
		{"running", authoredRegionFacts{Current: true, RenderExists: true}, AuthoredRegionRendering},
		{"completed_without_canonical", authoredRegionFacts{Current: true, RenderExists: true, RenderTerminal: true}, AuthoredRegionCancelling},
		{"failed_settled", authoredRegionFacts{Current: true, RenderExists: true, RenderTerminal: true, AllWritersClosed: true, AllQuotaSettled: true}, AuthoredRegionFailed},
		{"canonical_render", authoredRegionFacts{Current: true, RenderExists: true, RenderTerminal: true, RenderCanonical: true}, AuthoredRegionCropping},
		{"crop_unsettled", authoredRegionFacts{Current: true, RenderExists: true, RenderTerminal: true, RenderCanonical: true, CropExists: true, CropTerminal: true, CropCanonical: true}, AuthoredRegionCropping},
		{"ready", authoredRegionFacts{Current: true, RenderExists: true, RenderTerminal: true, RenderCanonical: true, CropExists: true, CropTerminal: true, CropCanonical: true, AllWritersClosed: true, AllQuotaSettled: true}, AuthoredRegionReady},
		{"cancel_open_writer", authoredRegionFacts{Current: true, CancelRequested: true, AllQuotaSettled: true}, AuthoredRegionCancelling},
		{"cancel_unsettled", authoredRegionFacts{Current: true, CancelRequested: true, AllWritersClosed: true}, AuthoredRegionCancelling},
		{"cancel_closed", authoredRegionFacts{Current: true, CancelRequested: true, AllWritersClosed: true, AllQuotaSettled: true}, AuthoredRegionCancelled},
		{"cancel_existing_children_closed", authoredRegionFacts{Current:true,CancelRequested:true,RenderExists:true,RenderTerminal:true,CropExists:true,CropTerminal:true,AllWritersClosed:true,AllQuotaSettled:true}, AuthoredRegionCancelled},
		{"stale_existing_children_closed", authoredRegionFacts{RenderExists:true,RenderTerminal:true,CropExists:true,CropTerminal:true,AllWritersClosed:true,AllQuotaSettled:true}, AuthoredRegionStale},
		{"stale_existing_writer", authoredRegionFacts{RenderExists:true,RenderTerminal:true,CropExists:true,AllQuotaSettled:true}, AuthoredRegionCancelling},
		{"stale_closed", authoredRegionFacts{AllWritersClosed: true, AllQuotaSettled: true}, AuthoredRegionStale},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			phase, err := nextAuthoredRegionPhase(c.facts)
			if err != nil || phase != c.want {
				t.Fatalf("got %s/%v want %s", phase, err, c.want)
			}
		})
	}
	for _, f := range []authoredRegionFacts{{RenderCanonical: true}, {RenderTerminal: true}, {CropExists: true}, {Current:true,RenderExists:true,CropExists:true}, {RenderExists: true, RenderCanonical: true}, {CropCanonical: true, CropTerminal: true, CropExists: true}} {
		if _, err := nextAuthoredRegionPhase(f); err == nil {
			t.Fatal("inconsistent canonical facts accepted")
		}
	}
}
