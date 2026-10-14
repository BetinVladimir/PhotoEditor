package provenance

import (
 "context"
 "crypto/sha256"
 "encoding/hex"
 "photoeditor/backend/internal/canonical"
 "photoeditor/backend/internal/v2input"

 "github.com/google/uuid"
 "github.com/jackc/pgx/v5/pgxpool"
)

// Registration is operator-qualified SQL state, never native attestation.
// LiveTrust must independently check current native/root trust; the isolated
// challenge endpoint independently checks its actual native operator as well.
type SQLSigningRegistrationTrust struct{
 DB *pgxpool.Pool
 TenantID, WorkerID uuid.UUID
 QualificationSHA string
 LiveTrust SigningTrustCheck
}
func(s SQLSigningRegistrationTrust)Check(ctx context.Context,policy SigningPolicy,chain []byte)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.TenantID==uuid.Nil||s.WorkerID==uuid.Nil||!digest(s.QualificationSHA)||s.LiveTrust==nil{return ErrInvalid}
 sum:=sha256.Sum256(chain);if len(chain)<1||len(chain)>1<<20||hex.EncodeToString(sum[:])!=policy.CertificateChainSHA{return ErrInvalid}
 // Freeze callback inputs before any SQL/native IO.
 body,err:=canonical.Marshal(policy);if err!=nil{return err}
 policySum:=sha256.Sum256(body);ref:=v2input.Ref{ArtifactID:policy.ID,SHA256:hex.EncodeToString(policySum[:])}
 raw,err:=DecodeSigningPolicy(body,ref,policy.ID);if err!=nil{return err}
 ownedChain:=append([]byte(nil),chain...)
 registered:=func()error{
  if err:=ctx.Err();err!=nil{return err}
  tx,err:=s.DB.Begin(ctx);if err!=nil{return err};defer tx.Rollback(ctx)
  if _,err=tx.Exec(ctx,`SELECT set_config('app.tenant_id',$1,true)`,s.TenantID.String());err!=nil{return err}
  var accepted bool
  if err=tx.QueryRow(ctx,`SELECT photo.v31_api_signer_registration_ready($1,$2,$3,$4,$5,$6)`,s.TenantID,s.WorkerID,raw.SDKLockSHA,raw.CertificateChainSHA,raw.TrustBundleSHA,s.QualificationSHA).Scan(&accepted);err!=nil{return err}
  if !accepted{return ErrInvalid};return tx.Commit(ctx)
 }
 if err=registered();err!=nil{return err}
 callback,err:=DecodeSigningPolicy(append([]byte(nil),body...),ref,raw.ID);if err!=nil{return err}
 if err=s.LiveTrust(ctx,callback,ownedChain);err!=nil{return err}
 return registered()
}

func(s SQLSigningRegistrationTrust)CheckInstallation(ctx context.Context)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||s.TenantID==uuid.Nil||s.WorkerID==uuid.Nil||!digest(s.QualificationSHA)||s.LiveTrust==nil{return ErrInvalid}
 var valid bool
 err:=s.DB.QueryRow(ctx,`SELECT current_user='photo_api' AND NOT caller.rolsuper AND NOT caller.rolbypassrls
 AND p.prosecdef AND owner.rolname='photo_fn_owner' AND NOT owner.rolsuper AND NOT owner.rolbypassrls
 AND 'search_path=pg_catalog, photo'=ANY(p.proconfig)
 AND has_function_privilege('photo_api',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_gateway',p.oid,'EXECUTE')
 AND NOT has_function_privilege('photo_portal',p.oid,'EXECUTE')
 AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) acl WHERE acl.grantee=0 AND acl.privilege_type='EXECUTE')
 FROM pg_proc p JOIN pg_roles owner ON owner.oid=p.proowner JOIN pg_roles caller ON caller.rolname=current_user
 WHERE p.oid=to_regprocedure('photo.v31_api_signer_registration_ready(uuid,uuid,text,text,text,text)')`).Scan(&valid)
 if err!=nil{return err};if !valid{return ErrInvalid};return ctx.Err()
}
