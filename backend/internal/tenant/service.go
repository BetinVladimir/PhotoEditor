package tenant

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
 "github.com/jackc/pgx/v5/pgconn"
	"photoeditor/backend/internal/auth"
	"photoeditor/backend/internal/canonical"
	"photoeditor/backend/internal/database"
	"photoeditor/backend/internal/domain"
	"photoeditor/backend/internal/idempotency"
	"photoeditor/backend/internal/pagination"
	"strconv"
	"strings"
	"time"
)

type Service struct {
	DB       *pgxpool.Pool
	Cursors  pagination.Codec
	MailKeys auth.KeyRing
}
type Tenant struct {
	ID        uuid.UUID       `json:"id"`
	Name      string          `json:"name"`
	Role      domain.Role     `json:"role"`
	Revision  string          `json:"revision"`
	Quotas    json.RawMessage `json:"quotas,omitempty"`
	Retention json.RawMessage `json:"retention,omitempty"`
}
type Member struct {
	ID       uuid.UUID   `json:"id"`
	UserID   uuid.UUID   `json:"user_id"`
	Email    string      `json:"email"`
	Role     domain.Role `json:"role"`
	Status   string      `json:"status"`
	Revision string      `json:"revision"`
}
type Quotas struct {
	StorageBytes      int64 `json:"storage_bytes"`
	SourceFiles       int64 `json:"source_files"`
	MaxActiveGPUTasks int   `json:"max_active_gpu_tasks"`
}
type Retention struct {
	ProcessingDays int `json:"processing_days"`
	ExportsDays    int `json:"exports_days"`
	TemporaryHours int `json:"temporary_hours"`
}
type CreateRequest struct {
	Name string `json:"name"`
}
type PatchRequest struct {
	Name      *string    `json:"name,omitempty"`
	Quotas    *Quotas    `json:"quotas,omitempty"`
	Retention *Retention `json:"retention,omitempty"`
}
type JobAccepted struct {
	JobID  uuid.UUID `json:"job_id"`
	Status string    `json:"status"`
}
type InviteRequest struct {
	Email  string      `json:"email"`
	Role   domain.Role `json:"role"`
	Locale string      `json:"locale"`
}
type InviteAccept struct {
	Token string `json:"token"`
}
type Invitation struct {
	ID        uuid.UUID   `json:"id"`
	Email     string      `json:"email"`
	Role      domain.Role `json:"role"`
	ExpiresAt time.Time   `json:"expires_at"`
}
type AcceptedMembership struct {
	ID       uuid.UUID   `json:"id"`
	TenantID uuid.UUID   `json:"tenant_id"`
	UserID   uuid.UUID   `json:"user_id"`
	Role     domain.Role `json:"role"`
	Status   string      `json:"status"`
	Revision string      `json:"revision"`
}

var defaultQuotas = Quotas{StorageBytes: 100 * 1024 * 1024 * 1024, SourceFiles: 100000, MaxActiveGPUTasks: 1}
var defaultRetention = Retention{ProcessingDays: 7, ExportsDays: 30, TemporaryHours: 24}

func validQuotas(value Quotas) bool {
	return value.StorageBytes >= 1024*1024 && value.StorageBytes <= 1<<50 && value.SourceFiles >= 1 && value.SourceFiles <= 1000000 && value.MaxActiveGPUTasks >= 0 && value.MaxActiveGPUTasks <= 16
}
func validRetention(value Retention) bool {
	return value.ProcessingDays >= 1 && value.ProcessingDays <= 365 && value.ExportsDays >= 1 && value.ExportsDays <= 365 && value.TemporaryHours >= 1 && value.TemporaryHours <= 168
}

func (s *Service) Invite(ctx context.Context, who auth.Identity, tid uuid.UUID, key string, request InviteRequest) (Invitation, error) {
	var out Invitation
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	email, err := auth.NormalizeEmail(request.Email)
	if err != nil || request.Role != domain.Admin && request.Role != domain.Editor && request.Role != domain.Viewer || request.Locale != "ru" && request.Locale != "bg" && request.Locale != "en" {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	request.Email = email
	hash, err := canonical.Hash(request)
	if err != nil {
		return out, err
	}
    // Accepted recovery precedes random-token generation and mail encryption.
    // The SQL reader checks the original actor/session/hash under receipt lock.
    var receiptOutcome string
    var receiptID *uuid.UUID
    var receiptEmail,receiptRole *string
    var receiptExpiry *time.Time
    err = s.DB.QueryRow(ctx,`SELECT outcome,invitation_id,email_normalized,role,expires_at FROM photo.read_invitation_receipt($1,$2,$3,$4,$5)`,who.UserID,who.SessionID,tid,key,hash).Scan(&receiptOutcome,&receiptID,&receiptEmail,&receiptRole,&receiptExpiry)
    if err != nil { return out,err }
    switch receiptOutcome {
    case "replayed":
        if receiptID==nil||*receiptID==uuid.Nil||receiptEmail==nil||*receiptEmail!=request.Email||receiptRole==nil||*receiptRole!=string(request.Role)||receiptExpiry==nil||receiptExpiry.IsZero(){return out,domain.NewError("IDEMPOTENCY_CONFLICT",409)}
        return Invitation{*receiptID,*receiptEmail,domain.Role(*receiptRole),*receiptExpiry},nil
    case "conflict":return out,domain.NewError("IDEMPOTENCY_CONFLICT",409)
    case "not_found":return out,domain.NewError("NOT_FOUND",404)
    case "missing":
    default:return out,domain.NewError("IDEMPOTENCY_CONFLICT",409)
    }
	token, err := auth.NewSecret(rand.Reader)
	if err != nil {
		return out, err
	}
	invitationID, payloadID, eventID := uuid.New(), uuid.New(), uuid.New()
	expires := time.Now().UTC().Add(7 * 24 * time.Hour)
	var tenantName string
    if err = WithTenant(ctx,s.DB,who,tid,domain.Admin,func(tx pgx.Tx,_ domain.Role)error{return tx.QueryRow(ctx,"SELECT name FROM photo.tenants WHERE id=$1",tid).Scan(&tenantName)});err!=nil{return out,err}
	kid, recipient, err := s.MailKeys.Seal([]byte(email), []byte(payloadID.String()+":recipient"))
	if err != nil {
		return out, err
	}
	body, _ := json.Marshal(map[string]string{"kind": "invite", "locale": request.Locale, "tenant": tenantName, "token": token})
	kid2, cipher, err := s.MailKeys.Seal(body, []byte(payloadID.String()+":payload"))
	if err != nil || kid2 != kid {
		return out, errors.New("mail encryption unavailable")
	}
	var outcome string
	err = s.DB.QueryRow(ctx, `SELECT outcome,invitation_id,email_normalized,role,expires_at FROM photo.create_invitation($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16)`, who.UserID, who.SessionID, tid, invitationID, email, request.Role, auth.HashSecret(token), expires, payloadID, recipient, cipher, kid, "<"+invitationID.String()+"@photoeditor.local>", eventID, key, hash).Scan(&outcome, &receiptID, &receiptEmail, &receiptRole, &receiptExpiry)
	if err != nil {
        var refusal *pgconn.PgError
        if errors.As(err,&refusal)&&refusal.Code=="42501"&&refusal.Message=="TENANT_ADMIN_AUTHORITY_CHANGED"{return Invitation{},domain.NewError("NOT_FOUND",404)}
        return out,err
    }
	switch outcome {
	case "created", "replayed":
        if receiptID==nil||*receiptID==uuid.Nil||receiptEmail==nil||*receiptEmail!=request.Email||receiptRole==nil||*receiptRole!=string(request.Role)||receiptExpiry==nil||receiptExpiry.IsZero(){return Invitation{},domain.NewError("IDEMPOTENCY_CONFLICT",409)}
        return Invitation{*receiptID,*receiptEmail,domain.Role(*receiptRole),*receiptExpiry},nil
	case "conflict":
		return Invitation{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	default:
		return Invitation{}, domain.NewError("NOT_FOUND", 404)
	}
}

func (s *Service) AcceptInvite(ctx context.Context, who auth.Identity, request InviteAccept) (AcceptedMembership, error) {
	var out AcceptedMembership
	if request.Token == "" || len(request.Token) > 128 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	var outcome string
	var membershipID, tenantID *uuid.UUID
	var role, status *string
	var revision *int64
	err := s.DB.QueryRow(ctx, "SELECT outcome,membership_id,tenant_id,role,status,revision FROM photo.accept_invitation($1,$2,$3)", who.UserID, who.SessionID, auth.HashSecret(request.Token)).Scan(&outcome, &membershipID, &tenantID, &role, &status, &revision)
	if err != nil {
		return out, err
	}
	if outcome != "accepted" {
		return AcceptedMembership{}, domain.NewError("INVITATION_INVALID", 404)
	}
	if membershipID == nil || *membershipID == uuid.Nil || tenantID == nil || *tenantID == uuid.Nil || role == nil || status == nil || revision == nil || *revision < 1 {
		return AcceptedMembership{}, domain.NewError("INVITATION_INVALID", 404)
	}
	out.ID, out.TenantID, out.Role, out.Status = *membershipID, *tenantID, domain.Role(*role), *status
	out.UserID = who.UserID
	out.Revision = strconv.FormatInt(*revision, 10)
	return out, nil
}

func (s *Service) Create(ctx context.Context, who auth.Identity, key string, request CreateRequest) (Tenant, error) {
	var out Tenant
	request.Name = strings.TrimSpace(request.Name)
	if !idempotency.ValidKey(key) {
		return out, domain.NewError("IDEMPOTENCY_KEY_REQUIRED", 400)
	}
	if request.Name == "" || len([]rune(request.Name)) > 120 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	hash, err := canonical.Hash(request)
	if err != nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	id := uuid.New()
	var outcome string
	var revision int64
	err = s.DB.QueryRow(ctx, "SELECT outcome,tenant_id,name,revision,quotas,retention FROM photo.create_tenant($1,$2,$3,$4,$5,$6,$7,$8)", who.UserID, who.SessionID, id, request.Name, defaultQuotas, defaultRetention, key, hash).Scan(&outcome, &out.ID, &out.Name, &revision, &out.Quotas, &out.Retention)
	if err != nil {
		return out, err
	}
	switch outcome {
	case "created", "replayed":
	case "conflict":
		return Tenant{}, domain.NewError("IDEMPOTENCY_CONFLICT", 409)
	case "unauthorized":
		return Tenant{}, domain.NewError("INVALID_CREDENTIALS", 401)
	default:
		return Tenant{}, errors.New("unknown tenant creation outcome")
	}
	out.Role, out.Revision = domain.Owner, strconv.FormatInt(revision, 10)
	return out, nil
}

func (s *Service) ListMembers(ctx context.Context, who auth.Identity, tid uuid.UUID, limit int, raw string) (pagination.Page[Member], error) {
	out := pagination.Page[Member]{Items: []Member{}}
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	scope := pagination.Cursor{Tenant: tid.String(), Principal: who.UserID.String(), Sort: "membership:id"}
	var after *uuid.UUID
	if raw != "" {
		cursor, err := s.Cursors.Decode(raw, scope)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, err := uuid.Parse(cursor.LastID)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		after = &id
	}
	err := WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, _ domain.Role) error {
		rows, err := tx.Query(ctx, `SELECT m.id,m.user_id,photo.tenant_member_email($4,$5,$1,m.user_id),m.role,m.status,m.revision
			FROM photo.memberships m
			WHERE m.tenant_id=$1 AND ($2::uuid IS NULL OR m.id>$2) ORDER BY m.id LIMIT $3`, tid, after, limit+1, who.UserID, who.SessionID)
		if err != nil {
			return err
		}
		defer rows.Close()
		for rows.Next() {
			var item Member
			var revision int64
			if err = rows.Scan(&item.ID, &item.UserID, &item.Email, &item.Role, &item.Status, &revision); err != nil {
				return err
			}
			item.Revision = strconv.FormatInt(revision, 10)
			out.Items = append(out.Items, item)
		}
		return rows.Err()
	})
	if err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		scope.LastID = out.Items[limit-1].ID.String()
		cursor, err := s.Cursors.Encode(scope)
		if err != nil {
			return out, err
		}
		out.NextCursor = &cursor
	}
	return out, nil
}

func authorize(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID) (domain.Role, error) {
	var role domain.Role
	var revision int64
	err := tx.QueryRow(ctx, "SELECT role,revision FROM photo.authorize_tenant($1,$2,$3)", who.UserID, who.SessionID, tid).Scan(&role, &revision)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", domain.NewError("NOT_FOUND", 404)
	}
	return role, err
}
// AuthorizeInTransaction rechecks live authority in the caller's tenant-scoped
// transaction. Call it again after a blocking mutation/receipt lock, rather than
// opening another transaction while the original transaction holds those locks.
func AuthorizeInTransaction(ctx context.Context, tx pgx.Tx, who auth.Identity, tid uuid.UUID, required domain.Role) (domain.Role, error) {
	role, err := authorize(ctx, tx, who, tid)
	if err != nil {
		return "", err
	}
	if !role.Allows(required) {
		return "", domain.NewError("FORBIDDEN", 403)
	}
	return role, nil
}

func WithTenant(ctx context.Context, pool *pgxpool.Pool, who auth.Identity, tid uuid.UUID, required domain.Role, fn func(pgx.Tx, domain.Role) error) error {
	return database.TenantTransaction(ctx, pool, tid, func(tx pgx.Tx) error {
		role, err := AuthorizeInTransaction(ctx, tx, who, tid, required)
		if err != nil {
			return err
		}
		authorizedTx := &authorityTransaction{Tx: tx, who: who, tenant: tid, required: required}
		if err = fn(authorizedTx, role); err != nil {
			return err
		}
		// A receipt/domain lock may have waited past revocation or expiry. Never
		// commit a mutation or expose its replay using only entry-time authority.
		_, err = AuthorizeInTransaction(ctx, tx, who, tid, required)
		return err
	})
}

// withAuthorityChange is deliberately private: the three membership operations
// below reauthorize after their parent lock and then may revoke their own role.
// Applying the ordinary postcondition would undo a valid self-demotion/removal.
func withAuthorityChange(ctx context.Context, pool *pgxpool.Pool, who auth.Identity, tid uuid.UUID, required domain.Role, fn func(pgx.Tx, domain.Role) error) error {
	return database.TenantTransaction(ctx, pool, tid, func(tx pgx.Tx) error {
		role, err := AuthorizeInTransaction(ctx, tx, who, tid, required)
		if err != nil { return err }
		return fn(tx, role)
	})
}
func (s *Service) List(ctx context.Context, who auth.Identity, limit int, raw string) (pagination.Page[Tenant], error) {
	out := pagination.Page[Tenant]{Items: []Tenant{}}
	if limit < 1 || limit > 100 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	scope := pagination.Cursor{Principal: who.UserID.String(), Sort: "tenant:id"}
	var after *uuid.UUID
	if raw != "" {
		c, err := s.Cursors.Decode(raw, scope)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		id, err := uuid.Parse(c.LastID)
		if err != nil {
			return out, domain.NewError("INVALID_CURSOR", 400)
		}
		after = &id
	}
	rows, err := s.DB.Query(ctx, "SELECT id,name,role,revision FROM photo.list_user_tenants($1,$2,$3,$4)", who.UserID, who.SessionID, after, limit+1)
	if err != nil {
		return out, err
	}
	defer rows.Close()
	for rows.Next() {
		var v Tenant
		var rev int64
		if err = rows.Scan(&v.ID, &v.Name, &v.Role, &rev); err != nil {
			return out, err
		}
		v.Revision = strconv.FormatInt(rev, 10)
		out.Items = append(out.Items, v)
	}
	if err = rows.Err(); err != nil {
		return out, err
	}
	if len(out.Items) > limit {
		out.Items = out.Items[:limit]
		scope.LastID = out.Items[limit-1].ID.String()
		cursor, err := s.Cursors.Encode(scope)
		if err != nil {
			return out, err
		}
		out.NextCursor = &cursor
	}
	return out, nil
}
func (s *Service) Get(ctx context.Context, who auth.Identity, tid uuid.UUID) (Tenant, error) {
	var out Tenant
	err := WithTenant(ctx, s.DB, who, tid, domain.Viewer, func(tx pgx.Tx, role domain.Role) error {
		var revision int64
		err := tx.QueryRow(ctx, "SELECT id,name,revision,quotas,retention FROM photo.tenants WHERE id=$1", tid).Scan(&out.ID, &out.Name, &revision, &out.Quotas, &out.Retention)
		out.Role = role
		out.Revision = strconv.FormatInt(revision, 10)
		return err
	})
	return out, err
}
func (s *Service) Rename(ctx context.Context, who auth.Identity, tid uuid.UUID, expected int64, name string) (Tenant, error) {
	var out Tenant
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 120 || expected < 1 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := WithTenant(ctx, s.DB, who, tid, domain.Admin, func(tx pgx.Tx, role domain.Role) error {
		var revision int64
		err := tx.QueryRow(ctx, "UPDATE photo.tenants SET name=$3,revision=revision+1 WHERE id=$1 AND revision=$2 RETURNING id,name,revision,quotas,retention", tid, expected, name).Scan(&out.ID, &out.Name, &revision, &out.Quotas, &out.Retention)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		out.Role = role
		out.Revision = strconv.FormatInt(revision, 10)
		return err
	})
	return out, err
}
func (s *Service) Update(ctx context.Context, who auth.Identity, tid uuid.UUID, expected int64, request PatchRequest) (Tenant, error) {
	var out Tenant
	if expected < 1 || request.Name == nil && request.Quotas == nil && request.Retention == nil {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	if request.Name != nil {
		trimmed := strings.TrimSpace(*request.Name)
		if trimmed == "" || len([]rune(trimmed)) > 120 {
			return out, domain.NewError("VALIDATION_FAILED", 422)
		}
		request.Name = &trimmed
	}
	if request.Quotas != nil && !validQuotas(*request.Quotas) || request.Retention != nil && !validRetention(*request.Retention) {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := WithTenant(ctx, s.DB, who, tid, domain.Admin, func(tx pgx.Tx, role domain.Role) error {
		var revision int64
		// Policy DTOs contain the editable canonical fields; historical/custom
		// stored metadata is not part of that DTO and must survive policy edits.
		err := tx.QueryRow(ctx, `UPDATE photo.tenants SET name=coalesce($3,name),quotas=CASE WHEN $4::jsonb IS NULL THEN quotas ELSE quotas || $4::jsonb END,retention=CASE WHEN $5::jsonb IS NULL THEN retention ELSE retention || $5::jsonb END,revision=revision+1
			WHERE id=$1 AND revision=$2 RETURNING id,name,revision,quotas,retention`, tid, expected, request.Name, request.Quotas, request.Retention).Scan(&out.ID, &out.Name, &revision, &out.Quotas, &out.Retention)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		out.Role, out.Revision = role, strconv.FormatInt(revision, 10)
		return err
	})
	return out, err
}

func (s *Service) Delete(ctx context.Context, who auth.Identity, tid uuid.UUID, expected int64) (JobAccepted, error) {
	out := JobAccepted{JobID: uuid.New(), Status: "queued"}
	if tid == uuid.Nil || expected < 1 {
		return JobAccepted{}, domain.NewError("VALIDATION_FAILED", 422)
	}
	var outcome string
    var jobID *uuid.UUID
	err := s.DB.QueryRow(ctx, "SELECT outcome,job_id FROM photo.request_tenant_deletion($1,$2,$3,$4,$5)", who.UserID, who.SessionID, tid, expected, out.JobID).Scan(&outcome, &jobID)
	if err != nil {
        var refusal *pgconn.PgError
        if errors.As(err,&refusal) && refusal.Code=="42501" && refusal.Message=="TENANT_ADMIN_AUTHORITY_CHANGED" { return JobAccepted{},domain.NewError("NOT_FOUND",404) }
		return JobAccepted{}, err
	}
	switch outcome {
	case "queued":
        if jobID==nil||*jobID==uuid.Nil{return JobAccepted{},errors.New("invalid tenant deletion receipt")}
        out.JobID=*jobID
		return out, nil
	case "deleting":
        if jobID==nil||*jobID==uuid.Nil{return JobAccepted{},errors.New("invalid tenant deletion receipt")}
        out.JobID=*jobID
		out.Status = "queued"
		return out, nil
	case "revision_conflict":
		return JobAccepted{}, domain.NewError("REVISION_CONFLICT", 409)
	case "not_found":
		return JobAccepted{}, domain.NewError("NOT_FOUND", 404)
	default:
		return JobAccepted{}, domain.NewError("REVISION_CONFLICT", 409)
	}
}
func (s *Service) TransferOwner(ctx context.Context, who auth.Identity, tid, target uuid.UUID, expected int64) error {
    if tid == uuid.Nil || target == uuid.Nil || expected < 1 { return domain.NewError("VALIDATION_FAILED",422) }
	return withAuthorityChange(ctx, s.DB, who, tid, domain.Owner, func(tx pgx.Tx, _ domain.Role) error {
		var current uuid.UUID
		var rev int64
		if err := tx.QueryRow(ctx, "SELECT owner_user_id,revision FROM photo.tenants WHERE id=$1 FOR UPDATE", tid).Scan(&current, &rev); err != nil {
			return err
		}
		if rev != expected {
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		if current != who.UserID {
			return domain.NewError("FORBIDDEN", 403)
		}
		if _, err := AuthorizeInTransaction(ctx, tx, who, tid, domain.Owner); err != nil {
			return err
		}
		if target == current {
			return nil
		}
		var status string
		err := tx.QueryRow(ctx, "SELECT status FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2 FOR UPDATE", tid, target).Scan(&status)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if status != "active" {
			return domain.NewError("MEMBER_INACTIVE", 409)
		}
		if _, err = AuthorizeInTransaction(ctx, tx, who, tid, domain.Owner); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.memberships SET role='admin',revision=revision+1 WHERE tenant_id=$1 AND user_id=$2", tid, current); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.memberships SET role='owner',revision=revision+1 WHERE tenant_id=$1 AND user_id=$2", tid, target); err != nil {
			return err
		}
		if _, err = tx.Exec(ctx, "UPDATE photo.tenants SET owner_user_id=$2,revision=revision+1 WHERE id=$1", tid, target); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,'tenant.transfer_owner','tenant',$1,$3,jsonb_build_object('target_user_id',$4::text))`, tid, who.UserID, uuid.New(), target.String())
		return err
	})
}
func (s *Service) UpdateMember(ctx context.Context, who auth.Identity, tid, target uuid.UUID, expected int64, newRole domain.Role, status string) (Member, error) {
	var out Member
	if newRole != domain.Admin && newRole != domain.Editor && newRole != domain.Viewer || status != "active" && status != "suspended" || expected < 1 {
		return out, domain.NewError("VALIDATION_FAILED", 422)
	}
	err := withAuthorityChange(ctx, s.DB, who, tid, domain.Admin, func(tx pgx.Tx, _ domain.Role) error {
		// Parent row serializes owner transfer and member mutations before membership locks.
		var owner uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT owner_user_id FROM photo.tenants WHERE id=$1 FOR UPDATE", tid).Scan(&owner); err != nil {
			return err
		}
		if owner == target {
			return domain.NewError("OWNER_PROTECTED", 409)
		}
		// Recheck permission after waiting for a concurrent demotion/transfer.
		role, err := authorize(ctx, tx, who, tid)
		if err != nil {
			return err
		}
		if !role.Allows(domain.Admin) {
			return domain.NewError("FORBIDDEN", 403)
		}
		// Own the target before the final authority check: UPDATE must not wait
		// on this membership using authority checked before session withdrawal.
		var targetID uuid.UUID
		err = tx.QueryRow(ctx, "SELECT user_id FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2 FOR UPDATE", tid, target).Scan(&targetID)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		if _, err = AuthorizeInTransaction(ctx, tx, who, tid, domain.Admin); err != nil {
			return err
		}
		// Resolve the authorized target before mutation: an admin may suspend or demote themself.
		err = tx.QueryRow(ctx, `SELECT photo.tenant_member_email($1,$2,$3,m.user_id) FROM photo.memberships m WHERE m.tenant_id=$3 AND m.user_id=$4`, who.UserID, who.SessionID, tid, target).Scan(&out.Email)
		if errors.Is(err, pgx.ErrNoRows) {
			return domain.NewError("NOT_FOUND", 404)
		}
		if err != nil {
			return err
		}
		var rev int64
		err = tx.QueryRow(ctx, `UPDATE photo.memberships m SET role=$4,status=$5,revision=m.revision+1 WHERE m.tenant_id=$1 AND m.user_id=$2 AND m.revision=$3 RETURNING m.id,m.user_id,m.role,m.status,m.revision`, tid, target, expected, newRole, status).Scan(&out.ID, &out.UserID, &out.Role, &out.Status, &rev)
		if errors.Is(err, pgx.ErrNoRows) {
			var exists bool
			if e := tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2)", tid, target).Scan(&exists); e != nil {
				return e
			}
			if !exists {
				return domain.NewError("NOT_FOUND", 404)
			}
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		out.Revision = strconv.FormatInt(rev, 10)
		return err
	})
	return out, err
}

func (s *Service) RemoveMember(ctx context.Context, who auth.Identity, tid, target uuid.UUID, expected int64) error {
	if target == uuid.Nil || expected < 1 {
		return domain.NewError("VALIDATION_FAILED", 422)
	}
	return withAuthorityChange(ctx, s.DB, who, tid, domain.Admin, func(tx pgx.Tx, _ domain.Role) error {
		var owner uuid.UUID
		if err := tx.QueryRow(ctx, "SELECT owner_user_id FROM photo.tenants WHERE id=$1 FOR UPDATE", tid).Scan(&owner); err != nil {
			return err
		}
		if owner == target {
			return domain.NewError("OWNER_PROTECTED", 409)
		}
		role, err := authorize(ctx, tx, who, tid)
		if err != nil || !role.Allows(domain.Admin) {
			if err != nil {
				return err
			}
			return domain.NewError("FORBIDDEN", 403)
		}
        // Own the target before the last authority check. The DELETE may
        // otherwise wait for a concurrent membership operation after checking
        // a session that expires or is withdrawn during that wait.
        var targetID uuid.UUID
        err = tx.QueryRow(ctx, "SELECT user_id FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2 FOR UPDATE",tid,target).Scan(&targetID)
        if errors.Is(err,pgx.ErrNoRows) { return domain.NewError("NOT_FOUND",404) }
        if err != nil { return err }
        if _,err = AuthorizeInTransaction(ctx,tx,who,tid,domain.Admin);err != nil { return err }
		result, err := tx.Exec(ctx, "DELETE FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2 AND revision=$3", tid, target, expected)
		if err != nil {
			return err
		}
		if result.RowsAffected() == 0 {
			var exists bool
			if err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT FROM photo.memberships WHERE tenant_id=$1 AND user_id=$2)", tid, target).Scan(&exists); err != nil {
				return err
			}
			if !exists {
				return domain.NewError("NOT_FOUND", 404)
			}
			return domain.NewError("REVISION_CONFLICT", 409)
		}
		_, err = tx.Exec(ctx, `INSERT INTO photo.audit_log(tenant_id,actor_type,actor_id,action,resource_type,resource_id,request_id,details) VALUES($1,'user',$2,'member.remove','membership',$3,$4,jsonb_build_object('user_id',$5::text))`, tid, who.UserID, target, uuid.New(), target.String())
		return err
	})
}
