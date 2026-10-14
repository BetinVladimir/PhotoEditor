package interchange

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

type personOriginInputPublisherStub struct {
	calls int
	wrong bool
}

func (s *personOriginInputPublisherStub) Publish(_ context.Context, _ PortablePersonOriginExpectation,
	attachment PreparedPersonOriginAttachment) (v2input.Ref, error) {
	s.calls++
	if s.wrong {
		return v2input.Ref{}, nil
	}
	return attachment.Ref, nil
}

func TestPublishedPersonOriginEntersExportInputDigestBeforeMaterialization(t *testing.T) {
	intent, compiled, snapshot, snapshotRaw := exportFreezeFixture(t)
	intent.Request.Format = "photoeditor_bundle"
	intent.RequestJCS, _ = canonical.Marshal(intent.Request)
	intent.RequestSHA = rawSHA(intent.RequestJCS)
	var old importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &old); err != nil {
		t.Fatal(err)
	}
	entry := old.Entries[0]
	var err error
	compiled, err = CompileExportPlan(context.Background(), intent.Identity.Workflow,
		intent.Request.ProjectID, "photoeditor_bundle", snapshot, snapshotRaw,
		ExportLimits{1 << 20, 1 << 20, 10}, exportFrameResolverFunc(func(context.Context, v2input.SessionSnapshotItem) (ExportFrame, error) {
			return ExportFrame{ExternalID: entry.External, Path: entry.Path, Content: entry.Content,
				PortableRecipe: *entry.Recipe, Rating: entry.Rating, Pick: entry.Pick, Crop: entry.Crop}, nil
		}))
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := FreezeExportDocuments(context.Background(), intent, compiled, snapshot, snapshotRaw)
	if err != nil {
		t.Fatal(err)
	}
	var plan importPlanDocument
	if err := decodeImportDocument(compiled.planBody, &plan); err != nil {
		t.Fatal(err)
	}
	selected := plan.Entries[0]
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: rawSHA([]byte(uuid.NewString()))}
	}
	scope := PortablePersonOriginExpectation{EntryID: selected.ID, Source: *selected.Binding,
		Recipe: *selected.Recipe, Override: ref(), MaskRef: ref(), MaskPixels: ref(),
		ModelLockSHA: rawSHA([]byte("model lock"))}
	claim := PortablePersonOriginClaim{SchemaVersion: 1, IssuerID: uuid.New(), EntryID: scope.EntryID,
		Source: scope.Source, Recipe: scope.Recipe, Override: scope.Override, MaskRef: scope.MaskRef,
		MaskPixels: scope.MaskPixels, ProducerTaskID: uuid.New(), ProducerAttempt: 1, ProducerFence: 1,
		Operation: "mask.person", OperationVersion: "1", ModelLockSHA: scope.ModelLockSHA,
		QualificationReport: ref()}
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyHash := sha256.Sum256(public)
	keyID := hex.EncodeToString(keyHash[:])
	claimBody, err := canonical.Marshal(claim)
	if err != nil {
		t.Fatal(err)
	}
	body, err := canonical.Marshal(PortablePersonOriginEnvelope{SchemaVersion: 1, Claim: claim,
		KeyID: keyID, SignatureHex: hex.EncodeToString(ed25519.Sign(private,
			append([]byte(portablePersonOriginDomain), claimBody...)))})
	if err != nil {
		t.Fatal(err)
	}
	path := "origins/" + scope.EntryID.String() + "/" + scope.MaskRef.ArtifactID.String() + ".json"
	receipt := v2input.Ref{ArtifactID: uuid.NewSHA1(scope.EntryID,
		[]byte("v30:person-origin:"+scope.MaskRef.ArtifactID.String())), SHA256: rawSHA(body)}
	attachment := PreparedPersonOriginAttachment{Ref: receipt, Path: path, Body: body,
		Mapping: importBundlePersonOrigin{EntryID: scope.EntryID, Recipe: scope.Recipe,
			Override: scope.Override, MaskRef: scope.MaskRef, MaskPixels: scope.MaskPixels,
			Path: path, SHA: receipt.SHA256}}
	entries := append([]v2input.Entry(nil), compiled.roots...)
	entries = append(entries, v2input.Entry{Ref: scope.Override, Contract: "Override"},
		v2input.Entry{Ref: scope.MaskPixels, Contract: "MaskPixels"})
	sort.Slice(entries, func(i, j int) bool { return entries[i].Ref.ArtifactID.String() < entries[j].Ref.ArtifactID.String() })
	digest, err := canonical.Hash(entries)
	if err != nil {
		t.Fatal(err)
	}
	base := ExportInputInventory{task: compiled.task, entries: entries, digest: digest}
	keys := map[PortablePersonOriginKey]ed25519.PublicKey{{IssuerID: claim.IssuerID, KeyID: keyID}: public}
	current := &originCurrentStub{}
	publisher := &personOriginInputPublisherStub{}
	newInventory, expected, err := PublishExportPersonOriginInputs(context.Background(), intent, frozen, base,
		[]ExportPersonOriginPublished{{Scope: scope, Attachment: attachment}}, publisher, keys, current)
	if err != nil || publisher.calls != 1 || current.calls != 2 || len(expected) != 1 ||
		len(newInventory.entries) != len(base.entries)+2 || newInventory.digest == base.digest ||
		newInventory.entries[0].Ref.ArtifactID == uuid.Nil {
		t.Fatal("published origin did not enter export task digest", err)
	}
	publisher.wrong = true
	if _, _, err := PublishExportPersonOriginInputs(context.Background(), intent, frozen, base,
		[]ExportPersonOriginPublished{{Scope: scope, Attachment: attachment}}, publisher, keys, current); err == nil {
		t.Fatal("unpublished receipt admitted")
	}
}
