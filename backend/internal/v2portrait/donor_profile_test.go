package v2portrait

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"github.com/google/uuid"
	"os"
	"photoeditor/backend/internal/artifactjson"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"reflect"
	"strings"
	"testing"
)

func TestDonorProfilePinnedImagesCapacityAndRuntime(t *testing.T) {
	raw, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var images []json.RawMessage
	if json.Unmarshal(raw, &images) != nil {
		t.Fatal("fixture")
	}
	image, err := review.ParseWorkingImage(images[0])
	if err != nil {
		t.Fatal(err)
	}
	image.Pixels.ID = uuid.New()
	if image.Alpha != nil {
		alpha := *image.Alpha
		alpha.ID = uuid.New()
		image.Alpha = &alpha
	}
	source, err := canonical.Marshal(image)
	if err != nil {
		t.Fatal(err)
	}
	raw, err = os.ReadFile("testdata/open-eyes-params-parity.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Params OpenEyesParams `json:"params"`
	}
	if json.Unmarshal(raw, &fixture) != nil {
		t.Fatal("params")
	}
	p := fixture.Params
	sum := sha256.Sum256(source)
	canvas, _ := canonical.Hash(image.Canvas)
	p.Base.Source.SHA256 = hex.EncodeToString(sum[:])
	p.Donor.Source.SHA256 = p.Base.Source.SHA256
	p.Base.CanvasHash = canvas
	p.Donor.CanvasHash = canvas
	face := p.DonorFaceID
	observation := uuid.New()
	donorBinding := p.Donor
	proof := DonorConfirmation{SchemaVersion: 1, PersonID: uuid.New(), Source: p.Base, Donor: &donorBinding, SourceFaceID: p.SourceFaceID, DonorFaceID: &face, SourceObservationID: uuid.New(), DonorObservationID: &observation, ConsentID: uuid.New(), Epoch: "1", ActorID: uuid.New()}
	proofRaw, proofSHA, err := proof.Compile()
	if err != nil {
		t.Fatal(err)
	}
	faceParams := p
	faceParams.Confirmation.SHA256 = proofSHA
	faceParams, sourceFaces, donorFaces := checkDonorFaceEvidence(t, faceParams, proofRaw)
	geometry, err := VerifyOpenEyesFacesFromManifests(faceParams, proofRaw, sourceFaces, donorFaces, source, source, .0001)
	if err != nil || geometry.SourceLandmarks[0] != [2]float64{float64(image.Canvas.Width) * .5, float64(image.Canvas.Height) * .5} {
		t.Fatal("manifest geometry", geometry, err)
	}
	corrupt := append([]byte(nil), source...)
	corrupt[0] ^= 1
	if got, err := VerifyOpenEyesFacesFromManifests(faceParams, proofRaw, sourceFaces, donorFaces, corrupt, source, .0001); err == nil || got != (DonorFaceGeometry{}) {
		t.Fatal("corrupt manifest geometry")
	}
	capacity, err := DonorPortraitResources(image.Canvas.Width, image.Canvas.Height, image.Canvas.Width, image.Canvas.Height, 1, 1)
	if err != nil {
		t.Fatal(err)
	}
	profile, err := CompileDonorExecutionProfile(p, source, source, 1, 1, strings.Repeat("a", 64), capacity)
	if err != nil || len(profile) == 0 {
		t.Fatal(string(profile), err)
	}
	var parsed struct {
		Resources DonorResourceEstimate `json:"resource_estimate"`
	}
	if json.Unmarshal(profile, &parsed) != nil || parsed.Resources != capacity {
		t.Fatal("profile resources")
	}
	imageInputs, err := DonorImageInputs(p, source, source)
	expectedInputs := 1
	if image.Alpha != nil {
		expectedInputs++
	}
	if err != nil || len(imageInputs) != expectedInputs {
		t.Fatal("physical inputs", imageInputs, err)
	}
	for _, entry := range imageInputs {
		if entry.Contract != "NpyArray" {
			t.Fatal("physical contract")
		}
	}
	aliased := p
	aliased.EyesMask.ArtifactID = image.Pixels.ID
	if got, err := DonorImageInputs(aliased, source, source); err == nil || got != nil {
		t.Fatal("JSON/binary alias admitted")
	}
	body, _, err := p.Compile()
	if err != nil {
		t.Fatal(err)
	}
	plan, err := CompileDonorTaskPlan(context.Background(), p.Base, p.Donor, source, source, body, 1, 1, strings.Repeat("a", 64), capacity)
	if err != nil || plan.Operation != "portrait.open_eyes" || len(plan.Inputs) != 7 || plan.ReservedBytes != capacity.OutputBytes || len(plan.PhysicalImageInputs) != expectedInputs {
		t.Fatal(plan, err)
	}
	if len(plan.PhysicalImageOccurrences) != 2*expectedInputs {
		t.Fatal("shared arrays lost version occurrences")
	}
	for i, occurrence := range plan.PhysicalImageOccurrences {
		expectedBinding := p.Base
		if i >= expectedInputs {
			expectedBinding = p.Donor
		}
		if occurrence.Binding != expectedBinding || occurrence.Parent != expectedBinding.Source || occurrence.Entry.Contract != "NpyArray" {
			t.Fatal("incorrect occurrence scope")
		}
	}
	scope := artifactjson.Scope{Tenant: uuid.New(), Run: uuid.New(), Owner: uuid.New(), AssemblyLease: uuid.New(), Purpose: "task_params"}
	for _, mode := range []string{"", "mutate", "wrong", "alias", "error"} {
		publisher := &localPlanPublisher{mode: mode, source: p.Donor.Source.ArtifactID}
		published, err := CompileAndPublishDonorTaskPlan(context.Background(), publisher, scope, p.Base, p.Donor, source, source, body, 1, 1, strings.Repeat("a", 64), capacity)
		if mode == "" || mode == "mutate" {
			if err != nil || published.Plan.ParamsSHA != plan.ParamsSHA || string(published.Plan.Params) != string(body) {
				t.Fatal("publication", mode, err)
			}
		} else if err == nil || published.Plan.Params != nil {
			t.Fatal("invalid publication accepted", mode)
		}
	}
	physicalAliasPublisher := &localPlanPublisher{mode: "alias", source: image.Pixels.ID}
	if _, err := CompileAndPublishDonorTaskPlan(context.Background(), physicalAliasPublisher, scope, p.Base, p.Donor, source, source, body, 1, 1, strings.Repeat("a", 64), capacity); err == nil {
		t.Fatal("Params aliased physical image")
	}
	badScope := scope
	badScope.LocalPortrait = true
	publisher := &localPlanPublisher{}
	if _, err := CompileAndPublishDonorTaskPlan(context.Background(), publisher, badScope, p.Base, p.Donor, source, source, body, 1, 1, strings.Repeat("a", 64), capacity); err == nil || publisher.calls != 0 {
		t.Fatal("wrong owner route published")
	}
	for i := 1; i < len(plan.Inputs); i++ {
		if plan.Inputs[i-1].ArtifactID.String() >= plan.Inputs[i].ArtifactID.String() {
			t.Fatal("unsorted inputs")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if got, err := CompileDonorTaskPlan(ctx, p.Base, p.Donor, source, source, body, 1, 1, strings.Repeat("a", 64), capacity); err == nil || got.Params != nil {
		t.Fatal("cancelled plan")
	}
	alias := p
	alias.SourceFaces = alias.EyesMask
	aliasRaw, _, _ := alias.Compile()
	if got, err := CompileDonorTaskPlan(context.Background(), p.Base, p.Donor, source, source, aliasRaw, 1, 1, strings.Repeat("a", 64), capacity); err == nil || got.Params != nil {
		t.Fatal("aliased direct inputs")
	}
	var object map[string]any
	_ = json.Unmarshal(body, &object)
	object["unknown"] = true
	badRaw, _ := canonical.Marshal(object)
	if got, err := CompileDonorTaskPlan(context.Background(), p.Base, p.Donor, source, source, badRaw, 1, 1, strings.Repeat("a", 64), capacity); err == nil || got.Params != nil {
		t.Fatal("extra Params fields")
	}
	for i := 0; i < reflect.TypeOf(capacity).NumField(); i++ {
		field := reflect.TypeOf(capacity).Field(i).Name
		t.Run(field, func(t *testing.T) {
			for _, overflow := range []bool{false, true} {
				limit := capacity
				value := reflect.ValueOf(&limit).Elem().Field(i)
				if overflow {
					value.SetInt(9007199254740992)
				} else {
					value.SetInt(value.Int() - 1)
				}
				if got, err := CompileDonorExecutionProfile(p, source, source, 1, 1, strings.Repeat("a", 64), limit); err == nil || got != nil {
					t.Fatal("invalid capacity admitted", field, overflow)
				}
			}
		})
	}
	for _, runtime := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63)} {
		if got, err := CompileDonorExecutionProfile(p, source, source, 1, 1, runtime, capacity); err == nil || got != nil {
			t.Fatal("invalid runtime admitted")
		}
	}
	donor := image
	donor.Color.ICCSHA256 = strings.Repeat("b", 64)
	donorRaw, err := canonical.Marshal(donor)
	if err != nil {
		t.Fatal(err)
	}
	sum = sha256.Sum256(donorRaw)
	mismatch := p
	mismatch.Donor.Source.SHA256 = hex.EncodeToString(sum[:])
	if got, err := CompileDonorExecutionProfile(mismatch, source, donorRaw, 1, 1, strings.Repeat("a", 64), capacity); err == nil || got != nil {
		t.Fatal("incompatible ICC admitted")
	}
	if _, err := VerifyDonorWorkingImage(p.Base, append(append([]byte{}, source...), byte(' '))); err == nil {
		t.Fatal("changed source accepted")
	}
	noncanonical := append(append([]byte{}, source...), byte(' '))
	sum = sha256.Sum256(noncanonical)
	binding := p.Base
	binding.Source.SHA256 = hex.EncodeToString(sum[:])
	if _, err := VerifyDonorWorkingImage(binding, noncanonical); err == nil {
		t.Fatal("noncanonical source accepted")
	}
}
