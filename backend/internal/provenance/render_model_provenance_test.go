package provenance

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2draft"
	"photoeditor/backend/internal/v2input"
	"strings"
	"testing"
)

type signingModelOriginFunc func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2draft.FrozenRenderLearning) (signingRenderModelOrigin, error)

func (f signingModelOriginFunc) read(c context.Context, w auth.Identity, r ReadySigningExport, t CanonicalSigningTask, l v2draft.FrozenRenderLearning) (signingRenderModelOrigin, error) {
	return f(c, w, r, t, l)
}

func TestSigningRenderModelOriginRequiresPhysicalTrainingIdentity(t *testing.T) {
	for _, mode := range []string{"valid", "profile", "training", "zero_training", "changed", "missing_document", "duplicate_document", "conflicting_document", "conflicting_object", "generic_witness", "changed_bytes", "wrong_pin", "catalogue_error", "cancelled", "model_lock", "icc_lock", "object_version_changed", "missing_occurrence", "unregistered_model", "extra_model", "duplicate_model"} {
		t.Run(mode, func(t *testing.T) {
			profile := uuid.New()
			training := strings.Repeat("a", 64)
			candidate := v2draft.CandidateProfile{SchemaVersion: 1, ProfileID: profile, TrainingSHA: training, FeatureOrder: "scene12_v1", RidgeLambda: .1}
			for i := range candidate.Scales {
				candidate.Scales[i] = 1
			}
			raw, err := canonical.Marshal(candidate)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(raw)
			pin := v2input.Ref{ArtifactID: uuid.New(), SHA256: hex.EncodeToString(sum[:])}
			learning := v2draft.FrozenRenderLearning{VersionID: uuid.New(), Model: pin}
			evidence := SigningRenderClosureEvidence{inventory: v2draft.TaskInputEvidence{
				Entries:     []v2input.Entry{{Ref: pin, Contract: "CandidateProfile"}},
				Documents:   []v2draft.DocumentWitness{{Contract: "CandidateProfile", Ref: pin, JCS: string(raw)}},
				Occurrences: []v2draft.OccurrenceWitness{{Node: v2draft.LayerRef{Contract: "CandidateProfile", Ref: pin, ExpectedLearning: &learning}}},
			}}
			switch mode {
			case "missing_occurrence":
				evidence.inventory.Occurrences = nil
			case "unregistered_model":
				evidence.inventory.Entries = nil
			case "extra_model":
				evidence.inventory.Entries = append(evidence.inventory.Entries, v2input.Entry{Contract: "CandidateProfile", Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: pin.SHA256}})
			case "duplicate_model":
				evidence.inventory.Entries = append(evidence.inventory.Entries, evidence.inventory.Entries[0])
			case "missing_document":
				evidence.inventory.Documents = nil
			case "duplicate_document":
				evidence.inventory.Documents = append(evidence.inventory.Documents, evidence.inventory.Documents[0])
			case "conflicting_document":
				duplicate := evidence.inventory.Documents[0]
				duplicate.JCS = "{}"
				evidence.inventory.Documents = append(evidence.inventory.Documents, duplicate)
			case "conflicting_object":
				duplicate := evidence.inventory.Documents[0]
				duplicate.Object.VersionID = "other"
				evidence.inventory.Documents = append(evidence.inventory.Documents, duplicate)
			case "generic_witness":
				evidence.inventory.Documents[0].Contract = "Artifact"
			case "changed_bytes":
				evidence.inventory.Documents[0].JCS += " "
			case "wrong_pin":
				learning.Model.ArtifactID = uuid.New()
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			calls := 0
			failure := errors.New("historical binding unavailable")
			reader := signingModelOriginFunc(func(context.Context, auth.Identity, ReadySigningExport, CanonicalSigningTask, v2draft.FrozenRenderLearning) (signingRenderModelOrigin, error) {
				calls++
				origin := signingRenderModelOrigin{Profile: profile, TrainingSHA: training, ModelLockSHA: strings.Repeat("c", 64), ICCLockSHA: strings.Repeat("d", 64)}
				switch mode {
				case "model_lock":
					origin.ModelLockSHA = strings.Repeat("e", 64)
				case "icc_lock":
					origin.ICCLockSHA = strings.Repeat("e", 64)
				case "profile":
					origin.Profile = uuid.New()
				case "training":
					origin.TrainingSHA = strings.Repeat("b", 64)
				case "zero_training":
					origin.TrainingSHA = strings.Repeat("0", 64)
				case "object_version_changed":
					if calls == 2 {
						origin.ObjectDigest = strings.Repeat("f", 64)
					}
				case "changed":
					if calls == 2 {
						origin.Profile = uuid.New()
					}
				case "catalogue_error":
					return signingRenderModelOrigin{}, failure
				}
				return origin, nil
			})
			err = verifySigningRenderModelOrigins(ctx, reader, auth.Identity{}, ReadySigningExport{}, CanonicalSigningTask{}, evidence, strings.Repeat("c", 64), strings.Repeat("d", 64))
			if mode == "valid" || mode == "duplicate_document" || mode == "generic_witness" {
				if err != nil || calls != 2 {
					t.Fatal("valid model rejected", err, calls)
				}
				return
			}
			if err == nil {
				t.Fatal("invalid model provenance accepted")
			}
			if mode == "catalogue_error" && !errors.Is(err, failure) {
				t.Fatal("catalogue error hidden", err)
			}
			if mode == "cancelled" && (calls != 0 || !errors.Is(err, context.Canceled)) {
				t.Fatal("cancellation ignored", err, calls)
			}
		})
	}
}

func TestSigningLearningJSONRequiresBoundedVersionedBytes(t *testing.T) {
	for _, mode := range []string{"valid", "hash", "length", "version", "too_large", "nil_stream", "open_error", "cancelled"} {
		t.Run(mode, func(t *testing.T) {
			raw := "{}"
			object := objectstore.Object{Key: "learning/evaluation", VersionID: "version-1", ContentType: "application/json", Bytes: 2, SHA256: bodyDigest([]byte(raw))}
			switch mode {
			case "hash":
				object.SHA256 = strings.Repeat("a", 64)
			case "length":
				object.Bytes = 3
			case "version":
				object.VersionID = "null"
			case "too_large":
				object.Bytes = (8 << 20) + 1
			}
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			if mode == "cancelled" {
				cancel()
			}
			calls := 0
			failure := errors.New("object unavailable")
			objects := signingDocumentObjectsFunc(func(context.Context, objectstore.Object) (io.ReadCloser, error) {
				calls++
				if mode == "nil_stream" {
					return nil, nil
				}
				if mode == "open_error" {
					return nil, failure
				}
				return io.NopCloser(strings.NewReader(raw)), nil
			})
			got, err := readSigningLearningJSON(ctx, objects, object)
			if mode == "valid" {
				if err != nil || string(got) != raw {
					t.Fatal("valid bytes rejected", err)
				}
				return
			}
			if err == nil || got != nil {
				t.Fatal("invalid learning object accepted")
			}
			if mode == "open_error" && !errors.Is(err, failure) {
				t.Fatal("object error hidden", err)
			}
			if (mode == "version" || mode == "too_large" || mode == "cancelled") && calls != 0 {
				t.Fatal("invalid request reached storage", calls)
			}
		})
	}
}
