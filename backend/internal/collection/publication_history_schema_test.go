package collection
import("context";"errors";"strings";"testing")
func TestPublicationHistoryStartupRequiresInstalledVersionKeyAndFencedPublisher(t *testing.T){
 for _,ready:=range []bool{false,true}{r:=&gallerySchemaReader{ready:ready};err:=RequireCollectionPublicationHistory(context.Background(),r);if(err==nil)!=ready||r.calls!=1||!r.bounded{t.Fatalf("ready=%v calls=%d bounded=%v err=%v",ready,r.calls,r.bounded,err)}
 for _,fragment:=range []string{"NOT EXISTS(SELECT FROM pg_constraint","asset_version_id","c.confdeltype='c'","immutable_content","FOR SHARE OF i","ON CONFLICT(tenant_id,item_id,asset_version_id)","p.prosecdef","a.grantee<>p.proowner"}{if !strings.Contains(r.query,fragment){t.Fatal("missing installed publication contract",fragment)}}}
}
func TestPublicationHistoryStartupCancellationAndDatabaseFailure(t *testing.T){
 ctx,cancel:=context.WithCancel(context.Background());cancel();r:=&gallerySchemaReader{ready:true};if e:=RequireCollectionPublicationHistory(ctx,r);!errors.Is(e,context.Canceled)||r.calls!=0{t.Fatal(e,r.calls)}
 ctx,cancel=context.WithCancel(context.Background());defer cancel();r=&gallerySchemaReader{ready:true,after:cancel};if e:=RequireCollectionPublicationHistory(ctx,r);!errors.Is(e,context.Canceled){t.Fatal(e)}
 failure:=errors.New("database unavailable");r=&gallerySchemaReader{err:failure};if e:=RequireCollectionPublicationHistory(context.Background(),r);!errors.Is(e,failure){t.Fatal(e)}
 if e:=RequireCollectionPublicationHistory(context.Background(),nil);e==nil{t.Fatal("nil database accepted")}
}

func TestPublicationHistoryAllowsOnlyMountedApplicationRoles(t *testing.T){
 for _,role:=range []string{"photo_api","photo_worker"}{r:=&gallerySchemaReader{ready:true};if e:=RequireCollectionPublicationHistoryForRole(context.Background(),r,role);e!=nil||r.calls!=1{t.Fatal(role,e,r.calls)}}
 for _,role:=range []string{"","photo_scheduler","photo_fn_owner","postgres"}{r:=&gallerySchemaReader{ready:true};if e:=RequireCollectionPublicationHistoryForRole(context.Background(),r,role);e==nil||r.calls!=0{t.Fatal(role,e,r.calls)}}
}
