package v2render

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"testing"
)

func TestMultiQualityIdentityFencesProducerResult(t *testing.T) {
	for _, op := range []string{"optical.panorama", "optical.focus_stack"} {
		binding, task, _, _ := originalInventoryFixture(t, op)
		pin := MultiPreviewSource{Tenant: uuid.New(), Project: uuid.New(), Producer: task, Binding: binding, Attempt: 1, Fence: 2, Principal: auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}}
		base, err := MultiQualityRunID(pin)
		if err != nil || base == binding.Run {
			t.Fatal(err)
		}
		again, err := MultiQualityRunID(pin)
		if err != nil || again != base {
			t.Fatal("unstable identity", err)
		}
		for _, kind := range []string{"attempt", "fence", "result", "sha"} {
			changed := pin
			switch kind {
			case "attempt":
				changed.Attempt++
			case "fence":
				changed.Fence++
			case "result":
				changed.Binding.ResultID = uuid.New()
			case "sha":
				changed.Binding.Result.SHA256 = "b" + changed.Binding.Result.SHA256[1:]
				if changed.Binding.Result.SHA256 == pin.Binding.Result.SHA256 {
					changed.Binding.Result.SHA256 = "c" + pin.Binding.Result.SHA256[1:]
				}
			}
			next, err := MultiQualityRunID(changed)
			if err != nil || next == base {
				t.Fatal("identity not fenced", kind, err)
			}
		}
	}
	if _, err := MultiQualityRunID(MultiPreviewSource{}); err == nil {
		t.Fatal("zero source accepted")
	}
}
