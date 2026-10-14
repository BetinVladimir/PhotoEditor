package v2render

import (
	"errors"
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"strings"
	"testing"
)

func TestRenderOccurrenceMetadataRequiresInventoryAndValidObject(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	record := v2draft.OccurrenceWitness{Node: v2draft.LayerRef{Ref: ref, Contract: "WorkingPixels", Path: "root.pixels"}, Object: objectstore.Object{Key: "pixels", VersionID: "v1", SHA256: ref.SHA256, Bytes: 128, ContentType: "application/x-npy"}}
	entries := []v2input.Entry{{Ref: ref, Contract: "NpyArray"}}
	if err := validateRenderOccurrences(RenderDraftRequest{ClosureOccurrences: []v2draft.OccurrenceWitness{record}}, entries); err != nil {
		t.Fatal(err)
	}
	for _, kind := range []string{"version", "bytes", "sha", "path", "inventory", "media", "json_role"} {
		changed := record
		inventory := entries
		switch kind {
		case "media":
			changed.Object.ContentType = "image/png"
		case "json_role":
			changed.Node.Contract = "Params"
			inventory = []v2input.Entry{{Ref: ref, Contract: "Params"}}
		case "version":
			changed.Object.VersionID = "null"
		case "bytes":
			changed.Object.Bytes = 0
		case "sha":
			changed.Object.SHA256 = strings.Repeat("b", 64)
		case "path":
			changed.Node.Path = ""
		case "inventory":
			inventory = []v2input.Entry{}
		}
		if err := validateRenderOccurrences(RenderDraftRequest{ClosureOccurrences: []v2draft.OccurrenceWitness{changed}}, inventory); !errors.Is(err, v2input.ErrInvalid) {
			t.Fatal("invalid occurrence accepted", kind, err)
		}
	}
}

func TestCloneRenderOccurrencesDetachesAuthorityPointers(t *testing.T) {
	binding := v2input.SessionVersionBinding{AssetVersionID: uuid.New()}
	learning := v2draft.FrozenRenderLearning{VersionID: uuid.New()}
	frame := v2draft.RenderFrameAuthority{SelectedVersion: uuid.New()}
	records := []v2draft.OccurrenceWitness{{Node: v2draft.LayerRef{ExpectedBinding: &binding, ExpectedLearning: &learning, RenderFrame: &frame}}}
	owned := cloneRenderOccurrences(records)
	binding.AssetVersionID = uuid.New()
	learning.VersionID = uuid.New()
	frame.SelectedVersion = uuid.New()
	if owned[0].Node.ExpectedBinding.AssetVersionID == binding.AssetVersionID || owned[0].Node.ExpectedLearning.VersionID == learning.VersionID || owned[0].Node.RenderFrame.SelectedVersion == frame.SelectedVersion {
		t.Fatal("frozen occurrence aliases authority pointers")
	}
}

func TestJSONOccurrenceRequiresItsOwnReadWitness(t *testing.T) {
	ref := v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	obj := objectstore.Object{Key: "json", VersionID: "v1", SHA256: ref.SHA256, Bytes: 2, ContentType: "application/json"}
	first := v2draft.OccurrenceWitness{Node: v2draft.LayerRef{Ref: ref, Contract: "Artifact", Path: "first"}, Object: obj}
	repeated := first
	repeated.Node.Path = "second"
	request := RenderDraftRequest{ClosureOccurrences: []v2draft.OccurrenceWitness{first, repeated}, ClosureDocuments: []v2draft.DocumentWitness{{Path: "first", Contract: "Artifact", Ref: ref, Object: obj, JCS: "{}"}}}
	entries := []v2input.Entry{{Ref: ref, Contract: "Artifact"}}
	if err := validateRenderOccurrences(request, entries); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("another path's read witness substituted", err)
	}
	witness := request.ClosureDocuments[0]
	witness.Path = "second"
	request.ClosureDocuments = append(request.ClosureDocuments, witness)
	if err := validateRenderOccurrences(request, entries); err != nil {
		t.Fatal("exact read witnesses rejected", err)
	}
}

func TestRenderOccurrenceReachabilityBoundsBinaryLeafDepth(t *testing.T) {
	for _, depth := range []int{32, 33} {
		nodes := map[string]v2draft.LayerRef{"root": {Contract: "FrozenRenderRecipe"}}
		edges := map[string][]string{}
		parent := "root"
		for i := 1; i <= depth; i++ {
			path := "child" + strconv.Itoa(i)
			contract := "Artifact"
			if i == depth {
				contract = "WorkingPixels"
			}
			nodes[path] = v2draft.LayerRef{Path: path, Contract: contract}
			edges[parent] = []string{path}
			parent = path
		}
		err := validateRenderOccurrenceReachability(nodes, edges, []string{"root"})
		if (depth == 32) != (err == nil) {
			t.Fatal("binary depth limit", depth, err)
		}
	}
	nodes := map[string]v2draft.LayerRef{"root": {}, "isolated": {}}
	if err := validateRenderOccurrenceReachability(nodes, map[string][]string{"isolated": {"isolated"}}, []string{"root"}); !errors.Is(err, v2input.ErrInvalid) {
		t.Fatal("isolated component accepted", err)
	}
}
