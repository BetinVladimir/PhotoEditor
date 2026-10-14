package v2draft

import (
	"context"
	"strings"
	"testing"

	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/v2input"
)

func TestGlareDonorPreparationRejectsClientAuthority(t *testing.T) {
	ref := func() v2input.Ref {
		return v2input.Ref{ArtifactID: uuid.New(), SHA256: strings.Repeat("a", 64)}
	}
	binding := func() v2input.SessionVersionBinding {
		return v2input.SessionVersionBinding{AssetVersionID: uuid.New(),
			Source: ref(), CanvasHash: strings.Repeat("b", 64)}
	}
	request := GlareDonorPreparationRequest{SchemaVersion: 1,
		Shoot: uuid.New(), Snapshot: uuid.New(), Ordinal: 0,
		SourceObservation: uuid.New(), DonorObservation: uuid.New(),
		Base: binding(), Donor: binding(), Strength: .5,
		GlassesMask: ref(), Protected: ref(), TargetPixel: ref(),
		ProtectedPixel: ref(), SourceFaces: ref(), DonorFaces: ref()}
	raw, err := canonical.Marshal(request)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := DecodeGlareDonorPreparationRequest(raw); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{
		append([]byte(" "), raw...),
		[]byte(`{"schema_version":1,"qualification":{},"snapshot_id":"` + request.Snapshot.String() + `"}`),
	} {
		if _, err := DecodeGlareDonorPreparationRequest(bad); err != v2input.ErrInvalid {
			t.Fatal("client authority or noncanonical request accepted", err)
		}
	}
	for _, mutate := range []func(*GlareDonorPreparationRequest){
		func(v *GlareDonorPreparationRequest) { v.Donor = v.Base },
		func(v *GlareDonorPreparationRequest) { v.TargetPixel = v.ProtectedPixel },
		func(v *GlareDonorPreparationRequest) { v.SourceFaces = v.DonorFaces },
		func(v *GlareDonorPreparationRequest) { v.ProtectedPixel.SHA256 = "invalid" },
		func(v *GlareDonorPreparationRequest) { v.Strength = 2 },
	} {
		changed := request
		mutate(&changed)
		body, err := canonical.Marshal(changed)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := DecodeGlareDonorPreparationRequest(body); err != v2input.ErrInvalid {
			t.Fatal("invalid donor preparation reached SQL", err)
		}
	}
	var service *GlareDonorSubmissionService
	if _, err := service.PrepareProjectKeyed(context.Background(), auth.Identity{
		UserID: uuid.New(), SessionID: uuid.New()}, uuid.New(), uuid.New(),
		"glare-prepare-1", request); err != v2input.ErrInvalid {
		t.Fatal("uninstalled preparation accepted", err)
	}
}
