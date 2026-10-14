package semanticproposal

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

func TestNativeQualifiedProducerSetRetainsOriginalUniqueRoots(t *testing.T) {
	first := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	second := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	original, err := nativeQualifiedProducerSet([]v2input.Ref{first, second})
	if err != nil {
		t.Fatal(err)
	}
	reverse, err := nativeQualifiedProducerSet([]v2input.Ref{second, first})
	if err != nil || reverse != original {
		t.Fatal("unstable set", err)
	}
	partial, err := nativeQualifiedProducerSet([]v2input.Ref{first})
	if err != nil || partial == original {
		t.Fatal("partial roots not distinguishable")
	}
	changed := first
	changed.SHA256 = strings.Repeat("c", 64)
	mixed, err := nativeQualifiedProducerSet([]v2input.Ref{changed, second})
	if err != nil || mixed == original {
		t.Fatal("changed original SHA not distinguishable")
	}
	for _, bad := range [][]v2input.Ref{{first, first}, {first, {}}, {{ArtifactID: uuid.New(), SHA256: "bad"}}} {
		if body, err := nativeQualifiedProducerSet(bad); err == nil || body != "" {
			t.Fatal("invalid/duplicate producer set accepted")
		}
	}
	if body, err := nativeQualifiedProducerSet(nil); err != nil || body != "[]" {
		t.Fatal("empty declared set", body, err)
	}
}

func TestNativeQualifiedProducerPairsRetainDetectorAssociation(t *testing.T) {
	root := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	other := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("b", 64)}
	detector := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("c", 64)}
	pairs := [][2]v2input.Ref{{root, detector}, {other, detector}}
	original, err := nativeQualifiedProducerPairs(pairs)
	if err != nil {
		t.Fatal("shared detector rejected", err)
	}
	reverse, err := nativeQualifiedProducerPairs([][2]v2input.Ref{pairs[1], pairs[0]})
	if err != nil || reverse != original {
		t.Fatal("unstable pairs", err)
	}
	changed := append([][2]v2input.Ref(nil), pairs...)
	changed[0][1] = v2input.Ref{ArtifactID: uuid.New(), SHA256: detector.SHA256}
	replacement, err := nativeQualifiedProducerPairs(changed)
	if err != nil || replacement == original {
		t.Fatal("detector substitution lost", err)
	}
	conflicting := detector
	conflicting.SHA256 = root.SHA256
	for _, bad := range [][][2]v2input.Ref{{pairs[0], pairs[0]}, {{root, root}}, {{root, detector}, {other, conflicting}}, {{root, {}}}} {
		if body, err := nativeQualifiedProducerPairs(bad); err == nil || body != "" {
			t.Fatal("invalid pairs accepted")
		}
	}
	if body, err := nativeQualifiedProducerPairs(nil); err != nil || body != "[]" {
		t.Fatal("empty pairs", body, err)
	}
}
