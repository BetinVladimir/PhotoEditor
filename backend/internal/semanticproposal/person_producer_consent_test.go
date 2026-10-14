package semanticproposal

import (
 "context"
 "os"
 "strings"
 "testing"
 "github.com/jackc/pgx/v5/pgxpool"
 "github.com/google/uuid"
 "photoeditor/backend/internal/auth"
 "photoeditor/backend/internal/v2input"
 "photoeditor/backend/internal/v2qualification"
)

// Installation coverage; positive lifecycle/revocation races require fixtures
// and are not established by inspecting a function definition.
func TestIntegrationPersonProducerConsentInstallation(t *testing.T){
 dsn:=os.Getenv("PHOTO_TEST_DATABASE_URL");if dsn==""{t.Skip("isolated database with migration01565 required")}
 ctx:=context.Background();db,err:=pgxpool.New(ctx,dsn);if err!=nil{t.Fatal(err)};defer db.Close()
 var definition,owner string;var definer,public,api bool
 err=db.QueryRow(ctx,`SELECT pg_get_functiondef(p.oid),pg_get_userbyid(p.proowner),p.prosecdef,
 EXISTS(SELECT 1 FROM aclexplode(coalesce(p.proacl,acldefault('f',p.proowner))) a WHERE a.grantee=0 AND a.privilege_type='EXECUTE'),
 has_function_privilege('photo_api',p.oid,'EXECUTE')
 FROM pg_proc p WHERE p.oid='photo.v30_person_profile_consent_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb)'::regprocedure`).Scan(&definition,&owner,&definer,&public,&api)
 if err!=nil{t.Fatal(err)};if owner!="photo_fn_owner"||!definer||public||!api{t.Fatal("person profile security changed")}
 for _,predicate:=range []string{"semantic_proposal_person_mask_origin","workflow_consent_links","consent_grants","consent_grant.status IS DISTINCT FROM 'granted'","consent_grant.epoch IS DISTINCT FROM consent_link.consent_epoch","consent_link.project_id IS DISTINCT FROM p_project","NOT run.cancel_requested","runtime_lock_sha","device_profile_sha","model_lock_sha"}{
  if !strings.Contains(definition,predicate){t.Fatal("missing person authority",predicate)}
 }
}

func TestPersonProducerProfileRejectsIncompleteInstallation(t *testing.T){
 reader:=SQLTransferSourceReader{}
 err:=reader.verifyPersonProducerProfile(context.Background(),auth.Identity{},uuid.Nil,v2input.SessionVersionBinding{},VerifiedPersonMask{},v2qualification.Expected{})
 if err!=v2qualification.ErrUnqualified{t.Fatal("missing database accepted",err)}
 ctx,cancel:=context.WithCancel(context.Background());cancel()
 err=reader.verifyPersonProducerProfile(ctx,auth.Identity{},uuid.Nil,v2input.SessionVersionBinding{},VerifiedPersonMask{},v2qualification.Expected{})
 if err!=context.Canceled{t.Fatal("cancelled authority did not stop",err)}
}
func TestPersonProducerProfileRefPins(t *testing.T){
 valid:=v2input.Ref{ArtifactID:uuid.New(),SHA256:strings.Repeat("a",64)}
 if !validPersonProfileRef(valid){t.Fatal("valid pin refused")}
 for _,ref:=range []v2input.Ref{{SHA256:valid.SHA256},{ArtifactID:valid.ArtifactID,SHA256:strings.Repeat("A",64)},{ArtifactID:valid.ArtifactID,SHA256:strings.Repeat("g",64)},{ArtifactID:valid.ArtifactID,SHA256:"short"}}{if validPersonProfileRef(ref){t.Fatal("invalid pin accepted")}}
}
