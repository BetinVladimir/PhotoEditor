package delivery

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/provenance"
	"photoeditor/backend/internal/v2input"
)

func TestDelegatedReceiptPhysicalVerificationRepeatsAuthority(t *testing.T) {
	sha := strings.Repeat("a", 64)
	now := time.Date(2026, 10, 13, 12, 0, 0, 0, time.UTC)
	policyID := uuid.New()
	policy := provenance.SigningPolicy{Schema: 1, ID: policyID,
		Revision: "1", SDKLockSHA: sha, KeyHandle: "operator-key",
		CertificateChainSHA: sha, TimestampMode: "off",
		AllowedMIME: []string{"image/jpeg"}, TrustBundleSHA: sha,
		RevocationMode: "off"}
	policyRaw, err := canonical.Marshal(policy)
	if err != nil { t.Fatal(err) }
	registry, err := provenance.NewSigningPolicyRegistry(provenance.SigningInstallation{
		SDKLockSHA: sha, CertificateChainSHA: sha, TrustBundleSHA: sha,
		KeyHandles: []string{"operator-key"}, Policies: []provenance.InstalledSigningPolicy{{
			Ref: v2input.Ref{ArtifactID: uuid.New(), SHA256: digest(policyRaw)},
			Body: policyRaw, NotBefore: now.Add(-time.Hour),
			NotAfter: now.Add(time.Hour)}},
	}, deliverySigningAuthority{})
	if err != nil { t.Fatal(err) }
	lease := ExpansionLease{Expansion: uuid.New(), Token: uuid.New(),
		Until: now.Add(time.Hour), Tenant: uuid.New(), Run: uuid.New(),
		Workflow: uuid.New(), Package: uuid.New(), ParentTask: uuid.New(),
		Cursor: 1, SlotCount: 1}
	actor, preset, exportID := uuid.New(), uuid.New(), uuid.New()
	original := v2input.Ref{ArtifactID: uuid.New(), SHA256: sha}
	slot := Slot{Ordinal: 0, PresetID: preset, State: "ready",
		ExportID: &exportID, Export: &original}
	key := uuid.NewSHA1(lease.Run, []byte("0:"+preset.String())).String()
	dep := SigningDependency{Ordinal: 0, PresetID: preset,
		ExportID: exportID, Export: original, PolicyID: policyID,
		IdempotencyKey: key}
	workflow := uuid.NewSHA1(lease.Tenant,
		[]byte("provenance:"+actor.String()+":"+key))
	task := uuid.NewSHA1(workflow, []byte("provenance.sign:0"))
	signedRaw, manifestRaw := []byte("signed-image"), []byte("c2pa-manifest")
	signedID, manifestID := uuid.New(), uuid.New()
	prefix := "tenants/"+lease.Tenant.String()+"/tasks/"+task.String()+
		"/attempts/1/fences/2/"
	receipt := SignedDependencyReceipt{
		Signed: v2input.Ref{ArtifactID: signedID, SHA256: digest(signedRaw)},
		Manifest: v2input.Ref{ArtifactID: manifestID, SHA256: digest(manifestRaw)},
		ValidationSHA: digest([]byte("validation")), TaskID: task,
		ResultID: uuid.New(), Bucket: "photos",
		SignedObject: objectstore.Object{Key: prefix+signedID.String(),
			VersionID: "v1", SHA256: digest(signedRaw),
			Bytes: int64(len(signedRaw)), ContentType: "image/jpeg"},
		ManifestObject: objectstore.Object{Key: prefix+manifestID.String(),
			VersionID: "v1", SHA256: digest(manifestRaw),
			Bytes: int64(len(manifestRaw)), ContentType: "application/c2pa"},
	}
	objects := &signedReceiptObjectStub{contents: map[string][]byte{
		receipt.SignedObject.Key: signedRaw,
		receipt.ManifestObject.Key: manifestRaw}}
	calls := 0
	read := func(context.Context) (SignedDependencyReceipt, bool, error) {
		calls++
		return receipt, true, nil
	}
	got, err := VerifyDelegatedSigningDependencyCurrent(context.Background(),
		lease, actor, slot, dep, digest(policyRaw), registry,
		func() time.Time { return now }, objects, read)
	if err != nil || got != receipt || calls != 2 || objects.calls != 2 {
		t.Fatal("exact adopted result rejected", err, calls, objects.calls)
	}
	calls = 0
	changed := func(context.Context) (SignedDependencyReceipt, bool, error) {
		calls++
		if calls == 2 { other := receipt; other.ValidationSHA = sha; return other, true, nil }
		return receipt, true, nil
	}
	if _, err := VerifyDelegatedSigningDependencyCurrent(context.Background(),
		lease, actor, slot, dep, digest(policyRaw), registry,
		func() time.Time { return now }, objects, changed); err == nil || calls != 2 {
		t.Fatal("changed receipt accepted", err, calls)
	}
}
