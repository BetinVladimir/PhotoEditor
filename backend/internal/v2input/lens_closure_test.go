package v2input

import (
	"context"
	"github.com/google/uuid"
	"math"
	"photoeditor/backend/internal/canonical"
	"strings"
	"testing"
)

func TestLensClosureFreezesOriginalAndNestedInputs(t *testing.T) {
	ref := func() Ref { return Ref{uuid.New(), strings.Repeat("a", 64)} }
	binding := SessionVersionBinding{uuid.New(), ref(), strings.Repeat("b", 64)}
	params := OpticalLensParams{1, binding, 1, ref(), 50, 2}
	sha, _ := canonical.Hash(params)
	paramsRef := Ref{uuid.New(), sha}
	original := LensOriginalBinding{binding, ref(), ".nef"}
	osha, _ := canonical.Hash(original)
	oref := Ref{uuid.New(), osha}
	source := CaptureImageInputs{Pixels: ref()}
	alpha := ref()
	source.Alpha = &alpha
	field, qualification := ref(), ref()
	task := uuid.New()
	closure, err := CompileLensClosure(task, params, paramsRef, source, []Ref{field}, qualification, original, oref)
	if err != nil || len(closure.Direct) != 9 {
		t.Fatal(len(closure.Direct), err)
	}
	cursor, err := closure.Open(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	roles := map[string]int{}
	for {
		entry, ok, err := cursor.Next(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		if !ok {
			break
		}
		roles[entry.Contract]++
	}
	if roles["OriginalImage"] != 1 || roles["OriginalLensBinding"] != 1 || roles["NpyArray"] != 3 || roles["QualificationReport"] != 1 {
		t.Fatal(roles)
	}
	changed := original
	changed.Original = ref()
	newSHA, _ := canonical.Hash(changed)
	newRef := Ref{oref.ArtifactID, newSHA}
	other, err := CompileLensClosure(task, params, paramsRef, source, []Ref{field}, qualification, changed, newRef)
	if err != nil || other.InputDigest == closure.InputDigest {
		t.Fatal("original replacement not reflected", err)
	}
	if _, err = CompileLensClosure(task, params, paramsRef, source, nil, qualification, changed, oref); err == nil {
		t.Fatal("unhashed binding accepted")
	}
	original.Binding.CanvasHash = strings.Repeat("c", 64)
	if _, err = CompileLensClosure(task, params, paramsRef, source, nil, qualification, original, oref); err == nil {
		t.Fatal("foreign canvas accepted")
	}
	params.Strength = math.NaN()
	if _, err = CompileLensClosure(task, params, paramsRef, source, nil, qualification, changed, newRef); err == nil {
		t.Fatal("NaN strength accepted")
	}
}
