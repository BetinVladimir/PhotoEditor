package provenance

import (
 "context"
 "math/big"
 "time"
 "github.com/jackc/pgx/v5/pgxpool"
)

type SQLSigningCRLCheckpoint struct{DB *pgxpool.Pool;TrustBundleSHA string}
func(s SQLSigningCRLCheckpoint)ObserveSigningCRL(ctx context.Context,issuer,number string,this time.Time,sha string)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||!digest(s.TrustBundleSHA)||!digest(issuer)||!digest(sha)||this.IsZero()||len(number)<1||len(number)>49{return ErrInvalid}
 value,ok:=new(big.Int).SetString(number,10)
 if !ok||value.Sign()<0||value.BitLen()>160||value.String()!=number{return ErrInvalid}
 var accepted bool
 if err:=s.DB.QueryRow(ctx,`SELECT photo.observe_v31_crl_checkpoint($1,$2,$3,$4,$5)`,s.TrustBundleSHA,issuer,number,this,sha).Scan(&accepted);err!=nil{return err}
 if !accepted{return ErrInvalid};return ctx.Err()
}
var _ SigningCRLCheckpoint=SQLSigningCRLCheckpoint{}

func(s SQLSigningCRLCheckpoint)CheckSigningCRLInstallation(ctx context.Context,trust string,issuers []string)error{
 if err:=ctx.Err();err!=nil{return err}
 if s.DB==nil||!digest(trust)||trust!=s.TrustBundleSHA||len(issuers)<1||len(issuers)>64{return ErrInvalid}
 owned:=append([]string(nil),issuers...);seen:=map[string]bool{}
 for _,issuer:=range owned{if !digest(issuer)||seen[issuer]{return ErrInvalid};seen[issuer]=true}
 var roleOK bool
 if err:=s.DB.QueryRow(ctx,`SELECT current_user='photo_api' AND NOT rolsuper AND NOT rolbypassrls
 AND NOT has_table_privilege(current_user,'photo.v31_crl_issuers','INSERT,UPDATE,DELETE,TRUNCATE')
 AND NOT has_table_privilege(current_user,'photo.v31_crl_checkpoints','INSERT,UPDATE,DELETE,TRUNCATE')
 FROM pg_roles WHERE rolname=current_user`).Scan(&roleOK);err!=nil{return err}
 if !roleOK{return ErrInvalid}
 for _,signature:=range []string{"photo.observe_v31_crl_checkpoint(text,text,text,timestamp with time zone,text)","photo.v31_crl_checkpoint_ready(text,text[])"}{
  var valid bool
  err:=s.DB.QueryRow(ctx,`SELECT p.prosecdef AND r.rolname='photo_fn_owner' AND NOT r.rolsuper AND NOT r.rolbypassrls
   AND 'search_path=pg_catalog, photo'=ANY(p.proconfig)
   AND has_function_privilege('photo_api',p.oid,'EXECUTE')
   AND NOT has_function_privilege('photo_worker',p.oid,'EXECUTE')
   AND NOT has_function_privilege('photo_scheduler',p.oid,'EXECUTE')
   AND NOT has_function_privilege('photo_gateway',p.oid,'EXECUTE')
   AND NOT has_function_privilege('photo_portal',p.oid,'EXECUTE')
   AND NOT EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE')
   FROM pg_proc p JOIN pg_roles r ON r.oid=p.proowner WHERE p.oid=to_regprocedure($1)`,signature).Scan(&valid)
  if err!=nil{return err};if !valid{return ErrInvalid}
 }
 var ready bool
 if err:=s.DB.QueryRow(ctx,`SELECT photo.v31_crl_checkpoint_ready($1,$2::text[])`,trust,owned).Scan(&ready);err!=nil{return err}
 if !ready{return ErrInvalid};return ctx.Err()
}
var _ SigningCRLInstallationCheckpoint=SQLSigningCRLCheckpoint{}
