package review

import "testing"

func TestOpenEyesDecisionUsesDedicatedSQLAuthority(t *testing.T) {
	if got := candidateDecisionFunction("portrait.open_eyes"); got != "photo.decide_v28_open_eyes_review" {
		t.Fatalf("open-eyes decision bypassed donor authority: %s", got)
	}
	for _, kind := range []string{"portrait.glare", "image.reflection"} {
		if got := candidateDecisionFunction(kind); got != "photo.decide_v28_restoration_review" {
			t.Fatalf("restoration decision route changed: %s", got)
		}
	}
}

func TestRelightDecisionUsesDedicatedSQLAuthority(t *testing.T) {
	if got := candidateDecisionFunction("image.relight"); got != "photo.decide_v28_relight_review" {
		t.Fatalf("relight decision bypassed source and qualification authority: %s", got)
	}
}
