package provenance

import (
	"github.com/google/uuid"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"testing"
)

func TestSigningRenderDocumentCoverageRejectsUnwitnessedControls(t *testing.T) {
	for _, mode := range []string{"valid", "missing", "duplicate", "conflicting_duplicate", "bytes", "contract", "version", "orphan", "missing_occurrence", "parent", "path", "occurrence_version", "unwitnessed_occurrence"} {
		t.Run(mode, func(t *testing.T) {
			evidence := SigningRenderClosureEvidence{}
			for _, contract := range []string{"FrozenRenderRecipe", "CorrectionPlan", "StyleV2", "Override", "LayerStack", "MaskRef"} {
				raw := "{}"
				pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest([]byte(raw))}
				evidence.inventory.Entries = append(evidence.inventory.Entries, v2input.Entry{Ref: pin, Contract: contract})
				evidence.inventory.Documents = append(evidence.inventory.Documents, v2draft.DocumentWitness{Ref: pin, Contract: contract, JCS: raw, Object: objectstore.Object{Key: contract, VersionID: "v1", SHA256: pin.SHA256, Bytes: 2, ContentType: "application/json"}})
			}
			for _, document := range evidence.inventory.Documents {
				evidence.inventory.Occurrences = append(evidence.inventory.Occurrences, v2draft.OccurrenceWitness{Node: v2draft.LayerRef{Path: document.Path, Contract: document.Contract, Ref: document.Ref, ParentRef: document.Parent}, Object: document.Object})
			}
			switch mode {
			case "missing_occurrence":
				evidence.inventory.Occurrences = evidence.inventory.Occurrences[:5]
			case "parent":
				evidence.inventory.Occurrences[0].Node.ParentRef = v2input.Ref{ArtifactID: uuid.New(), SHA256: bodyDigest([]byte("{}"))}
			case "path":
				evidence.inventory.Occurrences[0].Node.Path = "$other"
			case "occurrence_version":
				evidence.inventory.Occurrences[0].Object.VersionID = "v2"
			case "unwitnessed_occurrence":
				other := evidence.inventory.Occurrences[0]
				other.Node.Path = "$another"
				evidence.inventory.Occurrences = append(evidence.inventory.Occurrences, other)
			case "missing":
				evidence.inventory.Documents = evidence.inventory.Documents[:5]
			case "duplicate":
				evidence.inventory.Documents = append(evidence.inventory.Documents, evidence.inventory.Documents[0])
			case "conflicting_duplicate":
				duplicate := evidence.inventory.Documents[0]
				duplicate.Object.VersionID = "v2"
				evidence.inventory.Documents = append(evidence.inventory.Documents, duplicate)
			case "bytes":
				evidence.inventory.Documents[0].JCS = "[]"
			case "contract":
				evidence.inventory.Documents[0].Contract = "Override"
			case "version":
				evidence.inventory.Documents[0].Object.VersionID = "null"
			case "orphan":
				evidence.inventory.Entries = evidence.inventory.Entries[:5]
			}
			err := verifySigningRenderDocumentCoverage(evidence)
			if (err == nil) != (mode == "valid" || mode == "duplicate") {
				t.Fatal("incorrect document coverage", mode, err)
			}
		})
	}
}
