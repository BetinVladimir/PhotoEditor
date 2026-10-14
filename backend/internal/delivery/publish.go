package delivery

import (
	"bytes"
	"context"
	"encoding/json"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/tenant"
	"photoeditor/backend/internal/v2input"
	"strconv"
	"time"
)

type PublishRequest struct {
	ExpectedRevision string `json:"expected_revision"`
}

func (r *PublishRequest) UnmarshalJSON(raw []byte) error {
 fields,ok:=closedJSONObject(raw)
 if !ok||len(fields)!=1||len(fields["expected_revision"])==0||bytes.Equal(bytes.TrimSpace(fields["expected_revision"]),[]byte("null")){return domain.NewError("VALIDATION_FAILED",422)}
 var revision string
 if json.Unmarshal(fields["expected_revision"],&revision)!=nil{return domain.NewError("VALIDATION_FAILED",422)}
 r.ExpectedRevision=revision
 return nil
}

func PublishPackage(ctx context.Context, tx ProofTransaction, who auth.Identity, tid, id uuid.UUID, request PublishRequest, key string) (json.RawMessage, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	revision, err := ParsePublishRevision(request.ExpectedRevision)
	if tx == nil || who.UserID == uuid.Nil || who.SessionID == uuid.Nil || tid == uuid.Nil || id == uuid.Nil || err != nil || !idempotency.ValidKey(key) {
		return nil, domain.NewError("VALIDATION_FAILED", 422)
	}
	requestID := uuid.NewSHA1(id, []byte("delivery:publish:"+who.UserID.String()+":"+key))
	var publication *uuid.UUID
	if err := tx.QueryRow(ctx, `SELECT photo.publish_v30_delivery($1,$2,$3,$4,$5,$6)`, tid, id, who.UserID, who.SessionID, revision, requestID).Scan(&publication); err != nil {
		return nil, err
	}
	if publication == nil || *publication == uuid.Nil {
		return nil, domain.NewError("DELIVERY_PUBLICATION_STALE", 409)
	}
	body, err := ReadPackage(ctx, tx, who, tid, id)
	if err != nil {
		return nil, err
	}
	if err = validatePublishedPackage(body, tid, id, revision+1); err != nil {
		return nil, err
	}
	return body, ctx.Err()
}

func (s Service) PublishWithReceipt(ctx context.Context, who auth.Identity, tid, id uuid.UUID, request PublishRequest, key string) (idempotency.Response, error) {
	if s.DB == nil {
		return idempotency.Response{}, domain.NewError("DELIVERY_UNAVAILABLE", 503)
	}
	var receipt idempotency.Response
	err := tenant.WithTenant(ctx, s.DB, who, tid, domain.Editor, func(tx pgx.Tx, _ domain.Role) error {
		var authorized bool
		if err := tx.QueryRow(ctx, `SELECT photo.authorize_v30_delivery_publish($1,$2,$3,$4)`, tid, id, who.UserID, who.SessionID).Scan(&authorized); err != nil {
			return err
		}
		if !authorized {
			return domain.NewError("NOT_FOUND", 404)
		}
		response, err := idempotency.Use(ctx, tx, idempotency.Scope{Tenant: tid, Principal: who.UserID, PrincipalType: "user", Route: "POST /delivery-packages/" + id.String() + "/publish", Key: key}, request, time.Now().UTC().Add(24*time.Hour), func() (idempotency.Response, error) {
			body, err := PublishPackage(ctx, tx, who, tid, id, request, key)
			return idempotency.Response{Status: 200, Body: body, ResourceID: &id}, err
		})
        if err!=nil{return err}
        if err:=ValidatePublicationResponse(response,tid,id,request);err!=nil{return err}
        if err:=ctx.Err();err!=nil{return err}
		receipt = response
		receipt.Body = append(json.RawMessage(nil), response.Body...)
		return ctx.Err()
	})
	if err!=nil{return idempotency.Response{},err}
 return receipt, nil
}

func (s Service) Publish(ctx context.Context, who auth.Identity, tid, id uuid.UUID, request PublishRequest, key string) (json.RawMessage, error) {
	receipt, err := s.PublishWithReceipt(ctx, who, tid, id, request, key)
	return receipt.Body, err
}

// Validate the public receipt before the caller commits publication + audit.
// The SQL authority owns the transition; syntactically valid JSON alone cannot
// establish that its resource reader returned the same published package.
func validatePublishedPackage(raw []byte, tenantID, packageID uuid.UUID, revision int64) error {
	invalid := func() error { return domain.NewError("DELIVERY_PUBLICATION_INVALID", 500) }
	var resource struct {
		ID       uuid.UUID    `json:"id"`
		Revision string       `json:"revision"`
		Document *v2input.Ref `json:"document"`
		Data     struct {
			ID        uuid.UUID  `json:"id"`
			Tenant    uuid.UUID  `json:"tenant_id"`
			Revision  string     `json:"revision"`
			Type      string     `json:"resource_type"`
			State     string     `json:"state"`
			Manifest  uuid.UUID  `json:"manifest_artifact_id"`
			Published *time.Time `json:"published_at"`
		} `json:"data"`
	}
	if !ValidPackageResource(raw,tenantID,packageID) || json.Unmarshal(raw, &resource) != nil {
		return invalid()
	}
	expected := strconv.FormatInt(revision, 10)
	if tenantID == uuid.Nil || packageID == uuid.Nil || revision < 2 || revision > 9007199254740991 || resource.ID != packageID || resource.Revision != expected || resource.Data.ID != packageID || resource.Data.Tenant != tenantID || resource.Data.Revision != expected || resource.Data.Type != "delivery_packages" || resource.Data.State != "ready" || resource.Data.Published == nil || resource.Data.Published.IsZero() || resource.Document == nil || resource.Document.ArtifactID == uuid.Nil || resource.Document.ArtifactID != resource.Data.Manifest || !shaPattern.MatchString(resource.Document.SHA256) {
		return invalid()
	}
	return nil
}

// Cached acknowledgements are subject to the same public binding as fresh ones.
func ValidatePublicationResponse(response idempotency.Response,tid,id uuid.UUID,request PublishRequest)error{
 invalid:=func()error{return domain.NewError("DELIVERY_PUBLICATION_INVALID",500)}
 revision,err:=ParsePublishRevision(request.ExpectedRevision)
 if err!=nil||response.Status!=200||response.ResourceID==nil||*response.ResourceID!=id{return invalid()}
 return validatePublishedPackage(response.Body,tid,id,revision+1)
}

// Publication increments revision, so the highest safe integer is not an input.
func ParsePublishRevision(value string)(int64,error){
 revision,err:=strconv.ParseInt(value,10,64)
 if err!=nil||revision<1||revision>=9007199254740991||strconv.FormatInt(revision,10)!=value{return 0,domain.NewError("VALIDATION_FAILED",422)}
 return revision,nil
}
