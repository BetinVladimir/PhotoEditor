package v2optical

import (
	"context"
	"github.com/google/uuid"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/objectstore"
	"photoeditor/backend/internal/v2input"
)

// NegativeSourceArrayCatalog must resolve accepted source/child authority in
// a short transaction and commit before returning an immutable object version.
// The caller must repeat source locks inside the eventual admission transaction.
type NegativeSourceArrayCatalog interface {
	ResolveNegativeSourceArray(context.Context, auth.Identity, uuid.UUID, uuid.UUID, uuid.UUID, NegativeParams, []byte, NegativeSourceArraySpec) (objectstore.Object, error)
}

// VerifyNegativeSourceObjects streams each occurrence, including shared dense
// fields, and rechecks its live locator. Success is not an admission capability.
func VerifyNegativeSourceObjects(ctx context.Context, catalog NegativeSourceArrayCatalog, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, snapshot uuid.UUID, params NegativeParams, sourceRaw []byte) error {
 _,err:=readNegativeSourceProof(ctx,catalog,objects,who,tenant,project,snapshot,params,sourceRaw)
 return err
}

// This private receipt is issued only after all original bytes and repeated
// locators have been verified; it must be locked again in the sealing transaction.
type negativeSourceProof struct {
 who auth.Identity
 tenant,project,snapshot uuid.UUID
 params NegativeParams
 source string
 specs []NegativeSourceArraySpec
 objects []objectstore.Object
}

func readNegativeSourceProof(ctx context.Context, catalog NegativeSourceArrayCatalog, objects v2input.SessionAnalysisObjectReader, who auth.Identity, tenant, project, snapshot uuid.UUID, params NegativeParams, sourceRaw []byte) (*negativeSourceProof,error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if catalog == nil || objects == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tenant == uuid.Nil || project == uuid.Nil || snapshot == uuid.Nil {
		return nil, v2input.ErrInvalid
	}
	body, _, err := params.Compile()
	if err != nil {
		return nil, err
	}
	sourceRaw = append([]byte(nil), sourceRaw...)
	params, err = ParseNegativeParams(body)
	if err != nil {
		return nil, err
	}
	specs, err := NegativeSourceArrays(params, sourceRaw)
	if err != nil {
		return nil, err
	}
	// Each callback gets fresh owned bytes/slices: it cannot alter later checks.
	resolve := func(spec NegativeSourceArraySpec) (objectstore.Object, error) {
		if err := ctx.Err(); err != nil {
			return objectstore.Object{}, err
		}
		owned, err := ParseNegativeParams(body)
		if err != nil {
			return objectstore.Object{}, err
		}
		return catalog.ResolveNegativeSourceArray(ctx, who, tenant, project, snapshot, owned, append([]byte(nil), sourceRaw...), spec)
	}
	pinned := make([]objectstore.Object, len(specs))
	for i, spec := range specs {
		object, err := resolve(spec)
		if err != nil {
			return nil, err
		}
		reader, err := objects.Open(ctx, object)
		if err != nil {
			if reader != nil {
				_ = reader.Close()
			}
			return nil, err
		}
		if reader == nil {
			return nil, v2input.ErrInvalid
		}
		verifyErr := VerifyNegativeSourceArray(ctx, spec, object, reader)
		closeErr := reader.Close()
		if verifyErr != nil {
			return nil, verifyErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		current, err := resolve(spec)
		if err != nil {
			return nil, err
		}
		if current != object {
			return nil, v2input.ErrExpired
		}
		pinned[i] = object
	}
	for i, spec := range specs {
		current, err := resolve(spec)
		if err != nil {
			return nil, err
		}
		if current != pinned[i] {
			return nil, v2input.ErrExpired
		}
	}
	if err:=ctx.Err();err!=nil { return nil,err }
 return &negativeSourceProof{who:who,tenant:tenant,project:project,snapshot:snapshot,params:params,source:string(sourceRaw),specs:append([]NegativeSourceArraySpec(nil),specs...),objects:append([]objectstore.Object(nil),pinned...)},nil
}
