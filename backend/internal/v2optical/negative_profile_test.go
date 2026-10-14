package v2optical

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2qualification"
	"testing"
)

func TestNegativeQualifiedProfile(t *testing.T) {
	raw, err := os.ReadFile("testdata/negative-qualification.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		Params      NegativeParams
		Calibration string
		Report      string
		Pin         NegativeQualificationPin
		Locks       v2qualification.DeploymentLocks
	}
	if err = json.Unmarshal(raw, &f); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile("testdata/negative-source.json")
	if err != nil {
		t.Fatal(err)
	}
	image, err := review.ParseWorkingImage(source)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(source)
	f.Params.Base.Source.SHA256 = hex.EncodeToString(sum[:])
	f.Params.Base.CanvasHash, err = canonical.Hash(image.Canvas)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := VerifyNegativeQualificationEvidence(f.Pin, f.Locks, []byte(f.Report), []byte(f.Calibration), f.Params, image.Color.ICCSHA256)
	if err != nil {
		t.Fatal(err)
	}
	capacity, err := NegativeResources(image.Canvas.Width, image.Canvas.Height, image.Alpha != nil)
	if err != nil {
		t.Fatal(err)
	}
	body, err := CompileNegativeExecutionProfile(f.Params, source, evidence, capacity)
	if err != nil {
		t.Fatal(err)
	}

	paramsBody, paramsSHA, err := f.Params.Compile()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompileNegativeTaskPlan(context.Background(), f.Params.Base, paramsBody, source, evidence, capacity)
	if err != nil || plan.Operation != "optical.negative" || plan.ParamsSHA != paramsSHA || !bytes.Equal(plan.Params, paramsBody) || !bytes.Equal(plan.Profile, body) || len(plan.DirectInputs) != 3 || len(plan.PhysicalImageInputs) != 2 || len(plan.PhysicalImageOccurrences) != 2 || plan.ReservedBytes != capacity.OutputBytes {
		t.Fatal(plan, err)
	}
	published := v2input.Ref{ArtifactID: uuid.New(), SHA256: plan.ParamsSHA}
	entries, direct, inputErr := NegativeTaskInputEntries(context.Background(), plan, source, evidence, published)
	if inputErr != nil || len(entries) != 6 || len(direct) != 4 {
		t.Fatal(entries, direct, inputErr)
	}
	located, locErr := BindNegativeQualificationObjects(evidence, "bucket",
		objectstore.Object{SHA256: f.Pin.Report.SHA256, Bytes: int64(len(f.Report)), ContentType: "application/json", Key: "report", VersionID: "r1"},
		objectstore.Object{SHA256: f.Pin.Calibration.SHA256, Bytes: int64(len(f.Calibration)), ContentType: "application/json", Key: "calibration", VersionID: "c1"})
	if locErr != nil {
		t.Fatal(locErr)
	}
	for _, mode := range []string{"ok", "qualification_stale", "child_stale"} {
		tx := &negativeSourceScopeTx{negativeLockTx: negativeLockTx{mode: mode, tenant: uuid.New()}}
		lockedEntries, lockedDirect, lockErr := LockNegativeAdmission(context.Background(), tx, auth.Identity{UserID: uuid.New(), SessionID: uuid.New()}, tx.tenant, uuid.New(), uuid.New(), plan, source, located, published)
		if mode == "ok" && (lockErr != nil || len(lockedEntries) != 6 || len(lockedDirect) != 4) {
			t.Fatal(lockErr)
		}
		if mode != "ok" && (lockErr != v2input.ErrExpired || lockedEntries != nil || lockedDirect != nil) {
			t.Fatal("stale admitted", mode, lockErr)
		}
	}
	saved, savedErr := FreezeNegativeTaskDescriptor(context.Background(), plan, source, evidence, published)
	if savedErr != nil {
		t.Fatal(savedErr)
	}
	recovered, recoveredRef, recoveredSource, recoveryErr := RestoreNegativeTaskDescriptor(context.Background(), saved, evidence)
	if recoveryErr != nil || recoveredRef != published || !bytes.Equal(recoveredSource, source) || recovered.ParamsSHA != plan.ParamsSHA {
		t.Fatal(recoveryErr)
	}
	if _, _, _, err := RestoreNegativeTaskDescriptor(context.Background(), append(append([]byte{}, saved...), byte(' ')), evidence); err == nil {
		t.Fatal("noncanonical saved task")
	}
	var changedSaved negativeSavedTask
	if json.Unmarshal(saved, &changedSaved) != nil {
		t.Fatal("saved descriptor")
	}
	changedSaved.ReservedBytes++
	changedSavedRaw, _ := canonical.Marshal(changedSaved)
	if _, _, _, err := RestoreNegativeTaskDescriptor(context.Background(), changedSavedRaw, evidence); err == nil {
		t.Fatal("tampered recovery reserve")
	}
	owner := NegativeRecoveryOwner{TenantID: uuid.New(), ProjectID: uuid.New(), SnapshotID: uuid.New(), ActorID: uuid.New(), AuthSessionID: uuid.New(), RunID: uuid.New(), WorkflowID: uuid.New(), TaskID: uuid.New()}
	ownedRaw, ownedErr := FreezeOwnedNegativeRecovery(context.Background(), owner, plan, source, evidence, published)
	if ownedErr != nil {
		t.Fatal(ownedErr)
	}
	if _, ref, original, err := RestoreOwnedNegativeRecovery(context.Background(), ownedRaw, owner, evidence); err != nil || ref != published || !bytes.Equal(original, source) {
		t.Fatal(err)
	}
	for _, field := range []string{"tenant", "task", "snapshot", "session"} {
		changed := owner
		switch field {
		case "tenant":
			changed.TenantID = uuid.New()
		case "task":
			changed.TaskID = uuid.New()
		case "snapshot":
			changed.SnapshotID = uuid.New()
		case "session":
			changed.AuthSessionID = uuid.New()
		}
		if _, _, _, err := RestoreOwnedNegativeRecovery(context.Background(), ownedRaw, changed, evidence); err == nil {
			t.Fatal("scope substituted", field)
		}
	}
	publisher := &negativePublisher{}
	transport, transportErr := BuildNegativeTaskTransport(context.Background(), uuid.New(), plan, source, evidence, published, publisher)
	if transportErr != nil || transport.Built.Count != len(entries) || publisher.calls != 2 {
		t.Fatal(transport, transportErr, publisher.calls)
	}
	for round := 0; round < 2; round++ {
		cursor, err := transport.Open(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		for _, expected := range entries {
			entry, ok, err := cursor.Next(context.Background())
			if err != nil || !ok || entry != expected {
				t.Fatal(entry, ok, err)
			}
		}
		if _, ok, err := cursor.Next(context.Background()); ok || err != nil {
			t.Fatal(ok, err)
		}
	}
	failed := &negativePublisher{fail: true}
	if _, err := BuildNegativeTaskTransport(context.Background(), uuid.New(), plan, source, evidence, published, failed); err == nil || failed.calls != 1 {
		t.Fatal("publication failure lost", err)
	}
	aliasedPublisher := &negativePublisher{alias: published.ArtifactID}
	if graph, err := BuildNegativeTaskTransport(context.Background(), uuid.New(), plan, source, evidence, published, aliasedPublisher); err == nil || graph.ID != uuid.Nil {
		t.Fatal("input aliased by transport", graph, err)
	}
	changedBeforePublish := plan
	changedBeforePublish.ReservedBytes++
	untouched := &negativePublisher{}
	if _, err := BuildNegativeTaskTransport(context.Background(), uuid.New(), changedBeforePublish, source, evidence, published, untouched); err == nil || untouched.calls != 0 {
		t.Fatal("invalid plan reached publication", err)
	}
	changedPlan := plan
	changedPlan.ReservedBytes++
	if _, _, err := NegativeTaskInputEntries(context.Background(), changedPlan, source, evidence, published); err == nil {
		t.Fatal("changed plan accepted")
	}
	alias := published
	alias.ArtifactID = f.Params.Base.Source.ArtifactID
	if _, _, err := NegativeTaskInputEntries(context.Background(), plan, source, evidence, alias); err == nil {
		t.Fatal("aliased Params accepted")
	}
	changedBase := f.Params.Base
	changedBase.CanvasHash = f.Pin.ModelLockSHA
	if _, err := CompileNegativeTaskPlan(context.Background(), changedBase, paramsBody, source, evidence, capacity); err == nil {
		t.Fatal("substituted binding")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := CompileNegativeTaskPlan(cancelled, f.Params.Base, paramsBody, source, evidence, capacity); err != context.Canceled {
		t.Fatal(err)
	}
	paramsBody[0] ^= 1
	if plan.Params[0] != '{' {
		t.Fatal("mutable caller params")
	}
	var profile struct {
		Backend   string
		Renderer  string                   `json:"renderer_version"`
		Runtime   string                   `json:"negative_runtime_sha"`
		Pin       NegativeQualificationPin `json:"negative_qualification"`
		Resources NegativeResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(body, &profile) != nil || profile.Backend != "cpu" || profile.Renderer != "v2.2" || profile.Runtime != f.Pin.RuntimeLockSHA || profile.Pin != f.Pin || profile.Resources != capacity {
		t.Fatal(string(body))
	}

	aliased := image
	aliased.Pixels.ID = f.Params.Calibration.ArtifactID
	aliased.Pixels.SHA256 = f.Params.Calibration.SHA256
	aliasRaw, err := canonical.Marshal(aliased)
	if err != nil {
		t.Fatal(err)
	}
	aliasParams := f.Params
	aliasSum := sha256.Sum256(aliasRaw)
	aliasParams.Base.Source.SHA256 = hex.EncodeToString(aliasSum[:])
	if _, _, err := NegativeImageInputs(aliasParams, aliasRaw, f.Pin.Report); err == nil {
		t.Fatal("physical input aliases calibration")
	}
	for _, edge := range plan.PhysicalImageOccurrences {
		if edge.Parent != f.Params.Base.Source || edge.Binding != f.Params.Base || edge.Entry.Contract != "NpyArray" {
			t.Fatal(edge)
		}
	}
	for _, mode := range []string{"capacity", "pixels", "vram", "params", "source", "zero"} {
		p, c, e, s := f.Params, capacity, evidence, source
		switch mode {
		case "capacity":
			c.RAMBytes--
		case "pixels":
			c.PixelCount++
		case "vram":
			c.VRAMBytes = 1
		case "params":
			p.Strength = 0
		case "source":
			s = append(append([]byte(nil), source...), 32)
		case "zero":
			e = VerifiedNegativeQualification{}
		}
		if _, err := CompileNegativeExecutionProfile(p, s, e, c); err == nil {
			t.Fatal(mode)
		}
	}
}
