package v2portrait

import (
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"io"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
	"testing"
)

type donorObjectRepo struct {
	reservation DonorConfirmationReservation
	complete    int
}

func (r *donorObjectRepo) ReserveDonorConfirmation(_ context.Context, p DonorConfirmationPublication, _ int64) (DonorConfirmationReservation, error) {
	p.Prepared.Body[0] ^= 1
	return r.reservation, nil
}
func (r *donorObjectRepo) CompleteDonorConfirmation(_ context.Context, p DonorConfirmationPublication, _ DonorConfirmationReservation, _ objectstore.Object) (v2input.Ref, error) {
	r.complete++
	return v2input.Ref{ArtifactID: r.reservation.ArtifactID, SHA256: p.Prepared.SHA256}, nil
}

type donorObjectStore struct {
	body           []byte
	recover        bool
	wrong          bool
	puts, recovers int
}

func (s *donorObjectStore) PutImmutable(_ context.Context, key string, r io.Reader, n int64, sha, mime string) (objectstore.Object, error) {
	s.puts++
	s.body, _ = io.ReadAll(r)
	if s.recover {
		return objectstore.Object{}, objectstore.ErrUnavailable
	}
	return s.object(key, n, sha, mime), nil
}
func (s *donorObjectStore) RecoverImmutable(_ context.Context, key string, n int64, sha, mime string) (objectstore.Object, error) {
	s.recovers++
	return s.object(key, n, sha, mime), nil
}
func (s *donorObjectStore) object(key string, n int64, sha, mime string) objectstore.Object {
	v := "pinned"
	if s.wrong {
		v = "null"
	}
	return objectstore.Object{Key: key, Bytes: n, SHA256: sha, ContentType: mime, VersionID: v}
}

func checkDonorObjectPublisher(t *testing.T, request DonorConfirmationPublication) {
	t.Helper()
	checkDonorSQLRepository(t, request)
	contextBody, err := DonorIssuerContext(request)
	if err != nil {
		t.Fatal(err)
	}
	var contextValue donorIssuerContext
	if json.Unmarshal(contextBody, &contextValue) != nil || contextValue.Run != request.RunID || contextValue.Tenant != request.Tenant || contextValue.ProofJCS != string(request.Prepared.Body) || contextValue.Params != request.Prepared.Params {
		t.Fatal("issuer lineage lost")
	}
	changed := request
	changed.Actor.UserID = uuid.New()
	if body, err := DonorIssuerContext(changed); err == nil || body != nil {
		t.Fatal("wrong actor issuer context")
	}

	for _, mode := range []string{"ok", "recover", "wrong_object", "replay", "wrong_run"} {
		repo := &donorObjectRepo{reservation: DonorConfirmationReservation{RunID: request.RunID, ArtifactID: uuid.New(), Token: uuid.New()}}
		if mode == "wrong_run" {
			repo.reservation.RunID = uuid.New()
		}
		store := &donorObjectStore{recover: mode == "recover", wrong: mode == "wrong_object"}
		if mode == "replay" {
			repo.reservation.Ready = &v2input.Ref{ArtifactID: repo.reservation.ArtifactID, SHA256: request.Prepared.SHA256}
		}
		ref, err := (DonorConfirmationObjectPublisher{Repository: repo, Objects: store}).PublishDonorConfirmation(context.Background(), request)
		if mode == "wrong_run" {
			if err == nil || ref != (v2input.Ref{}) || store.puts != 0 || repo.complete != 0 {
				t.Fatal("cross-run reservation uploaded")
			}
			continue
		}
		if mode == "wrong_object" {
			if err == nil || ref != (v2input.Ref{}) || repo.complete != 0 {
				t.Fatal("invalid object completed")
			}
			continue
		}
		if err != nil || ref.ArtifactID != repo.reservation.ArtifactID || ref.SHA256 != request.Prepared.SHA256 {
			t.Fatal(mode, err)
		}
		if mode == "replay" {
			if store.puts != 0 || repo.complete != 0 {
				t.Fatal("replay performed upload")
			}
		} else if string(store.body) != string(request.Prepared.Body) || repo.complete != 1 {
			t.Fatal("repository mutated uploaded bytes")
		}
		if mode == "recover" && store.recovers != 1 {
			t.Fatal("missing recovery")
		}
	}
}
