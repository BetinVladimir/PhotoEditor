package v2input

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"testing"

	"bytes"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"io"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/review"
)

type lensSelectionRows struct {
	pgx.Rows
	values []lensSelectedRow
	index  int
	closed bool
}

func (r *lensSelectionRows) Next() bool { r.index++; return r.index <= len(r.values) }
func (r *lensSelectionRows) Close()     { r.closed = true }
func (r *lensSelectionRows) Err() error { return nil }
func (r *lensSelectionRows) Scan(d ...any) error {
	v := r.values[r.index-1]
	*d[0].(*int) = v.ordinal
	*d[1].(*uuid.UUID) = v.version
	*d[2].(*uuid.UUID) = v.source.ArtifactID
	*d[3].(*string) = v.source.SHA256
	*d[4].(*int64) = v.sourceObject.Bytes
	*d[5].(*string) = v.sourceBucket
	*d[6].(*string) = v.sourceObject.Key
	*d[7].(*string) = v.sourceObject.VersionID
	*d[8].(*string) = v.sourceObject.ContentType
	*d[9].(*uuid.UUID) = v.original.Ref.ArtifactID
	*d[10].(*string) = v.original.Ref.SHA256
	*d[11].(*int64) = v.original.Object.Bytes
	*d[12].(*string) = v.originalBucket
	*d[13].(*string) = v.original.Object.Key
	*d[14].(*string) = v.original.Object.VersionID
	*d[15].(*string) = v.original.Object.ContentType
	*d[16].(*int64) = v.count
	return nil
}

type lensSelectionDB struct{ rows *lensSelectionRows }

func (d lensSelectionDB) Query(context.Context, string, ...any) (pgx.Rows, error) { return d.rows, nil }

type lensSelectionObjects struct {
	body  []byte
	opens int
}

func (o *lensSelectionObjects) Open(context.Context, objectstore.Object) (io.ReadCloser, error) {
	o.opens++
	return io.NopCloser(bytes.NewReader(o.body)), nil
}

func TestLensSelectionContentAndWholeSetValidation(t *testing.T) {
	fixtures, err := os.ReadFile("../review/testdata/working-images-python.json")
	if err != nil {
		t.Fatal(err)
	}
	var docs []json.RawMessage
	if err = json.Unmarshal(fixtures, &docs); err != nil {
		t.Fatal(err)
	}
	raw := docs[0]
	image, err := review.ParseWorkingImage(raw)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	row := lensSelectedRow{ordinal: 0, version: uuid.New(), source: Ref{uuid.New(), hex.EncodeToString(sum[:])}, sourceBucket: "photos", sourceObject: objectstore.Object{Key: "source", VersionID: "v1", ContentType: "application/json", Bytes: int64(len(raw))}, originalBucket: "photos", original: LensOriginalSource{Ref: Ref{uuid.New(), image.Canvas.SourceSHA256}, Object: objectstore.Object{Key: "original", VersionID: "v1", Bytes: 100, ContentType: "image/jpeg"}}, count: 1}
	tenant, project, snapshot, actor, session := uuid.New(), uuid.New(), uuid.New(), uuid.New(), uuid.New()
	run := func(rows []lensSelectedRow, body []byte) ([]LensSelectedSource, error, int) {
		cursor := &lensSelectionRows{values: rows}
		objects := &lensSelectionObjects{body: body}
		source := SQLLensSelectionSource{DB: lensSelectionDB{cursor}, Objects: objects, Bucket: "photos"}
		result, err := source.Load(context.Background(), tenant, project, snapshot, actor, session)
		if !cursor.closed {
			t.Fatal("catalog cursor leaked")
		}
		return result, err, objects.opens
	}
	result, err, opens := run([]lensSelectedRow{row}, raw)
	if err != nil || opens != 1 || len(result) != 1 || result[0].Binding.Source != row.source || result[0].Original.Original != row.original.Ref || !bytes.Equal(result[0].SourceJSON, raw) {
		t.Fatal("verified selection", result, err)
	}

	profile := LensProfileDocument{SchemaVersion: 1, CameraModel: "Camera", LensModel: "Lens", FocalMM: 50, Aperture: 2, CX: .5, CY: .5, K: []float64{0, 0, 0}, P: []float64{0, 0}, Vignette: []float64{0, 0, 0}, CARedScale: 1, CABlueScale: 1, Qualification: Ref{uuid.New(), row.source.SHA256}}
	profileJSON, _ := canonical.Marshal(profile)
	profileSHA, _ := canonical.Hash(profile)
	choice := LensProfileChoice{row.version, 1, Ref{uuid.New(), profileSHA}, profileJSON}
	workflow := uuid.New()
	limits := LensAdmissionLimits{32 << 30, 32 << 30, 32 << 30}
	requests, compileErr := CompileLensSelection(workflow, result, []LensProfileChoice{choice}, limits)
	if compileErr != nil || len(requests) != 1 || requests[0].Params.Base != result[0].Binding || requests[0].Params.FocalMM != 50 {
		t.Fatal("selection compile", compileErr)
	}
	replay, compileErr := CompileLensSelection(workflow, result, []LensProfileChoice{choice}, limits)
	if compileErr != nil || replay[0].ParamsRef != requests[0].ParamsRef || replay[0].OriginalRef != requests[0].OriginalRef {
		t.Fatal("unstable provisional documents", compileErr)
	}

	documents := &replayableLensDocuments{}
	draftStore := &lensDraftStoreFake{}
	taskStage := &lensStageDB{accepted: true}
	inputs := &lensRegistrarFake{}
	sealDB := &lensSealDB{accepted: true}
	var coefficients map[string]json.RawMessage
	_ = json.Unmarshal(profileJSON, &coefficients)
	delete(coefficients, "qualification")
	coefficientsSHA, _ := canonical.Hash(coefficients)
	pin := LensQualificationPin{profile.Qualification, coefficientsSHA, row.source.SHA256, row.source.SHA256}
	for name, pins := range map[string][]LensQualificationPin{"missing": nil, "duplicate": {pin, pin}, "other report": {{Ref{uuid.New(), row.source.SHA256}, coefficientsSHA, row.source.SHA256, row.source.SHA256}}} {
		t.Run(name, func(t *testing.T) {
			if _, err := VerifyQualifiedLensProfile(profileJSON, choice.Profile, row.source.SHA256, pins); err == nil {
				t.Fatal("unqualified profile admitted")
			}
		})
	}
	if _, err := VerifyQualifiedLensProfile(profileJSON, choice.Profile, profileSHA, []LensQualificationPin{pin}); err == nil {
		t.Fatal("runtime substitution accepted")
	}
	coordinator := LensAdmissionCoordinator{Selection: lensSelectionLoaderFake{result}, Profiles: lensProfileLoaderFake{profileJSON}, RuntimeLockSHA: row.source.SHA256, QualificationPins: []LensQualificationPin{pin}, Drafts: LensDraftCoordinator{Store: draftStore, Preparation: LensPreparationCoordinator{Documents: documents, Stage: SQLLensTaskStage{DB: taskStage}, Inputs: inputs}}, Sealer: SQLLensDraftStore{DB: sealDB}}
	admission := LensAdmissionRequest{Tenant: tenant, Actor: actor, Project: project, Snapshot: snapshot, Session: session, Key: "selected-lens", PolicySHA: row.source.SHA256, Choices: []LensProfileChoice{choice}, Limits: limits}
	admission.Choices[0].ProfileJSON = []byte(`{"caller":"untrusted"}`)
	ticket, admitErr := coordinator.Admit(context.Background(), admission)
	expectedRun, _ := RunID(tenant, actor, admission.Key)
	if admitErr != nil || ticket.RunID != expectedRun || ticket.State != "queued" || sealDB.calls != 1 || inputs.calls != 1 {
		t.Fatal("joined admission", ticket, admitErr)
	}
	sealDB.accepted = false
	if ticket, err := coordinator.Admit(context.Background(), admission); err == nil || ticket.RunID != uuid.Nil {
		t.Fatal("activation rejection returned success", ticket, err)
	}
	before := draftStore.calls
	admission.Limits = LensAdmissionLimits{1, 1, 1}
	if _, err := coordinator.Admit(context.Background(), admission); err == nil || draftStore.calls != before {
		t.Fatal("invalid selection wrote draft")
	}

	// Compiled inputs must own their bytes across caller mutations.
	originalByte := profileJSON[0]
	profileJSON[0] = '!'
	if requests[0].ProfileJSON[0] != originalByte {
		t.Fatal("profile compiler retained mutable caller bytes")
	}
	profileJSON[0] = originalByte
	wrongChoice := choice
	wrongChoice.AssetVersionID = uuid.New()
	if value, err := CompileLensSelection(workflow, result, []LensProfileChoice{wrongChoice}, limits); err == nil || value != nil {
		t.Fatal("foreign profile selection accepted")
	}
	wrongChoice = choice
	wrongChoice.Profile = Ref{uuid.New(), row.source.SHA256}
	if value, err := CompileLensSelection(workflow, result, []LensProfileChoice{wrongChoice}, limits); err == nil || value != nil {
		t.Fatal("profile hash substitution accepted")
	}
	if value, err := CompileLensSelection(workflow, result, []LensProfileChoice{choice}, LensAdmissionLimits{1, 1, 1}); err == nil || value != nil {
		t.Fatal("resource admission bypass")
	}
	secondSource := result[0]
	secondSource.Ordinal = 1
	secondSource.Binding.AssetVersionID = uuid.New()
	secondSource.Original.Binding = secondSource.Binding
	secondChoice := choice
	secondChoice.AssetVersionID = secondSource.Binding.AssetVersionID
	secondChoice.Strength = 0
	batch := []LensSelectedSource{result[0], secondSource}
	ordered, err := CompileLensSelection(workflow, batch, []LensProfileChoice{secondChoice, choice}, limits)
	if err != nil || len(ordered) != 2 || ordered[0].Params.Strength != 1 || ordered[1].Params.Strength != 0 {
		t.Fatal("choices bound by request order", err)
	}
	if value, err := CompileLensSelection(workflow, batch, []LensProfileChoice{choice, choice}, limits); err == nil || value != nil {
		t.Fatal("duplicate profile assignment accepted")
	}

	if _, err, opens = run(nil, raw); !errors.Is(err, ErrExpired) || opens != 0 {
		t.Fatal("empty authorized selection", err, opens)
	}
	for name, mutate := range map[string]func(*lensSelectedRow){
		"missing row":         func(r *lensSelectedRow) { r.count = 2 },
		"null source version": func(r *lensSelectedRow) { r.sourceObject.VersionID = "null" },
		"other bucket":        func(r *lensSelectedRow) { r.originalBucket = "other" },
		"wrong ordinal":       func(r *lensSelectedRow) { r.ordinal = 1 },
	} {
		t.Run(name, func(t *testing.T) {
			copy := row
			mutate(&copy)
			if _, err, opens := run([]lensSelectedRow{copy}, raw); err == nil || opens != 0 {
				t.Fatal("catalog defect reached IO", err, opens)
			}
		})
	}
	wrong := row
	wrong.original.Ref.SHA256 = hex.EncodeToString(sum[:])
	if _, err, _ = run([]lensSelectedRow{wrong}, raw); err == nil {
		t.Fatal("original substitution accepted")
	}
	tampered := append([]byte(nil), raw...)
	tampered[len(tampered)-1] = ' '
	if _, err, _ = run([]lensSelectedRow{row}, tampered); err == nil {
		t.Fatal("source SHA substitution accepted")
	}
	second := row
	second.ordinal = 1
	second.count = 2
	first := row
	first.count = 2
	if _, err, opens = run([]lensSelectedRow{first, second}, raw); err == nil || opens != 0 {
		t.Fatal("duplicate version reached IO", err, opens)
	}
}

type lensSelectionLoaderFake struct{ sources []LensSelectedSource }

func (s lensSelectionLoaderFake) Load(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID) ([]LensSelectedSource, error) {
	return s.sources, nil
}

type lensProfileLoaderFake struct{ body []byte }

func (s lensProfileLoaderFake) Load(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, uuid.UUID, Ref) ([]byte, error) {
	return s.body, nil
}
