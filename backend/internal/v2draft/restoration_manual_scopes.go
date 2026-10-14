package v2draft

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/review"
	"photoeditor/backend/internal/v2input"
	"photoeditor/backend/internal/v2portrait"
)

// RestorationManualScopes carries the exact original producer root/Params.
// It is origin evidence, not a substitute for replay of the original vectors
// or for live admission when the graph is sealed.
type RestorationManualScopes struct {
	root, original              v2input.Ref
	targetPixel, protectedPixel v2input.Ref
	target, protected           LayerRef
	verified                    bool
}

func (s RestorationManualScopes) RootRef() v2input.Ref           { return s.root }
func (s RestorationManualScopes) OriginalParamsRef() v2input.Ref { return s.original }
func (s RestorationManualScopes) TargetPixelRef() v2input.Ref    { return s.targetPixel }
func (s RestorationManualScopes) ProtectedPixelRef() v2input.Ref { return s.protectedPixel }

// LockRestorationManualScopes derives both mask scopes from current SQL
// producer/result authority, binding the physical NPY refs from canonical
// MaskRef bytes. The short transaction ends before object IO; final admission
// must recheck live producer and mask authority.
func LockRestorationManualScopes(ctx context.Context, tx pgx.Tx,
	who auth.Identity, tenant, project, snapshot uuid.UUID,
	base v2input.SessionVersionBinding, sourceRaw, targetRaw, guardRaw []byte,
	targetRef, guardRef v2input.Ref) (RestorationManualScopes, error) {
	var zero RestorationManualScopes
	if err := ctx.Err(); err != nil {
		return zero, err
	}
	if tx == nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil ||
		who.UserID == uuid.Nil || who.SessionID == uuid.Nil ||
		!isDocumentRef(targetRef) || !isDocumentRef(guardRef) || targetRef == guardRef {
		return zero, v2input.ErrInvalid
	}
	source, err := v2portrait.VerifyDonorWorkingImage(base, sourceRaw)
	if err != nil {
		return zero, v2input.ErrInvalid
	}
	parse := func(raw []byte, ref v2input.Ref, protected bool) (review.MaskRef, error) {
		var empty review.MaskRef
		if len(raw) < 2 || len(raw) > 1048576 {
			return empty, v2input.ErrInvalid
		}
		sum := sha256.Sum256(raw)
		if hex.EncodeToString(sum[:]) != ref.SHA256 {
			return empty, v2input.ErrInvalid
		}
		mask, e := review.ParseMaskRef(raw)
		if e != nil {
			return empty, v2input.ErrInvalid
		}
		exact, e := canonical.Marshal(mask)
		if e != nil || !bytes.Equal(exact, raw) || mask.Protected != protected ||
			mask.Canvas != source.Canvas || !bytes.Equal(mask.TransformChain, source.TransformChain) {
			return empty, v2input.ErrInvalid
		}
		return mask, nil
	}
	target, err := parse(targetRaw, targetRef, false)
	if err != nil {
		return zero, err
	}
	guard, err := parse(guardRaw, guardRef, true)
	if err != nil || target.Artifact.ID == guard.Artifact.ID {
		return zero, v2input.ErrInvalid
	}
	var role string
	if err = tx.QueryRow(ctx, "SELECT current_user").Scan(&role); err != nil {
		return zero, err
	}
	if role != "photo_scheduler" {
		return zero, v2input.ErrInvalid
	}
	if _, err = tx.Exec(ctx, "SELECT set_config('app.tenant_id',$1,true)", tenant.String()); err != nil {
		return zero, err
	}
	baseRaw, err := canonical.Marshal(base)
	if err != nil {
		return zero, err
	}
	var rootID, originalID uuid.UUID
	var rootSHA, originalSHA string
	err = tx.QueryRow(ctx, "SELECT root_id,root_sha,params_id,params_sha FROM photo.v28_manual_pair_origin($1,$2,$3,$4,$5,$6::jsonb,$7,$8,$9,$10,$11,$12,$13,$14)",
		tenant, project, snapshot, who.UserID, who.SessionID, string(baseRaw),
		targetRef.ArtifactID, targetRef.SHA256, guardRef.ArtifactID, guardRef.SHA256,
		target.Artifact.ID, target.Artifact.SHA256, guard.Artifact.ID, guard.Artifact.SHA256).
		Scan(&rootID, &rootSHA, &originalID, &originalSHA)
	if errors.Is(err, pgx.ErrNoRows) {
		return zero, v2input.ErrExpired
	}
	if err != nil {
		return zero, err
	}
	root := v2input.Ref{ArtifactID: rootID, SHA256: rootSHA}
	original := v2input.Ref{ArtifactID: originalID, SHA256: originalSHA}
	if !isDocumentRef(root) || !isDocumentRef(original) || root == original ||
		root.ArtifactID == targetRef.ArtifactID || root.ArtifactID == guardRef.ArtifactID {
		return zero, v2input.ErrInvalid
	}
	if err = ctx.Err(); err != nil {
		return zero, err
	}
	return RestorationManualScopes{root: root, original: original, verified: true,
		targetPixel:    v2input.Ref{ArtifactID: target.Artifact.ID, SHA256: target.Artifact.SHA256},
		protectedPixel: v2input.Ref{ArtifactID: guard.Artifact.ID, SHA256: guard.Artifact.SHA256},
		target: LayerRef{Path: "TaskInputs.target", Contract: "MaskRef", Ref: targetRef,
			CanvasHash: base.CanvasHash, ExpectedBinding: &base},
		protected: LayerRef{Path: "TaskInputs.protected", Contract: "MaskRef", Ref: guardRef,
			CanvasHash: base.CanvasHash, ExpectedBinding: &base}}, nil
}
