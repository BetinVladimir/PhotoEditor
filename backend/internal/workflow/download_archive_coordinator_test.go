package workflow

import (
    "context"
    "crypto/sha256"
    "encoding/hex"
    "reflect"
    "strings"
    "testing"
    "github.com/google/uuid"
    "github.com/jackc/pgx/v5"
    "photoeditor/backend/internal/artifactjson"
)

type archiveCoordinatorContextDB struct { items []downloadArchiveRow; events *[]string; opened *[]*archiveContextRows }
func (d archiveCoordinatorContextDB) Query(context.Context, string, ...any) (pgx.Rows, error) {
    *d.events = append(*d.events, "load")
    rows := &archiveContextRows{items: append([]downloadArchiveRow(nil), d.items...)}
    *d.opened = append(*d.opened, rows)
    return rows, nil
}

func TestArchiveCoordinatorCompleteOrderedLifecycleAndStableRetryIdentity(t *testing.T) {
    for _, cancelled := range []bool{false, true} {
        t.Run(map[bool]string{false:"complete_repeat", true:"cancel_materialize"}[cancelled], func(t *testing.T) {
            snapshot, tenant, run := uuid.New(), uuid.New(), uuid.New()
            items := []downloadArchiveRow{}
            for index := 0; index < 2; index++ {
                items = append(items, downloadArchiveRow{snapshot: snapshot, snapshotDigest: strings.Repeat("a",64),
                    volume:index+1, kind:"zip", ordinal:int64(index), artifact:uuid.New(), sha:strings.Repeat("b",64),
                    bytes:1, name:"frame.jpg", producer:uuid.New()})
            }
            events := []string{}
            opened := []*archiveContextRows{}
            owners, pins := []uuid.UUID{}, []string{}
            ctx, cancel := context.WithCancel(context.Background())
            defer cancel()
            db := archiveRegistrationCallbackDB{callback:func(_ context.Context, query string, _ ...any) pgx.Row {
                phase := "unknown"
                if strings.Contains(query,"register_download_archive_plan") { phase="register" }
                if strings.Contains(query,"materialize_download_archive_task") { phase="materialize" }
                if strings.Contains(query,"seal_download_archives") { phase="seal" }
                events=append(events,phase)
                return archiveRegistrationCallbackRow{callback:func(values ...any) error {
                    *(values[0].(*bool))=true
                    if cancelled && phase=="materialize" { cancel() }
                    return nil
                }}
            }}
            publisher := archivePublicationCallback(func(_ context.Context, scope artifactjson.Scope, body []byte)(artifactjson.Ref,error){
                events=append(events,"publish")
                if scope.Tenant!=tenant || scope.Run!=run || scope.Purpose!="task_params" { t.Fatal("publication scope changed") }
                hash:=sha256.Sum256(body)
                pin:=hex.EncodeToString(hash[:])
                owners=append(owners,scope.Owner);pins=append(pins,pin)
                return artifactjson.Ref{ID:uuid.NewSHA1(scope.Owner,[]byte(scope.Purpose)),SHA256:pin},nil
            })
            coordinator:=DownloadArchiveCoordinator{
                Source:DownloadArchiveContextSource{DB:archiveCoordinatorContextDB{items:items,events:&events,opened:&opened}},
                Builder:DownloadArchiveBuilder{Plans:DownloadArchiveRepository{DB:db},Publisher:publisher},
                Tasks:DownloadArchiveTaskRepository{DB:db}}
            attempts:=2
            if cancelled { attempts=1 }
            for attempt:=0;attempt<attempts;attempt++ {
                events=nil
                ok,err:=coordinator.Build(ctx,tenant,run)
                expected:=[]string{"load","register","register","publish","publish","materialize","materialize","seal"}
                if cancelled {
                    expected=expected[:6]
                    if ok || err!=context.Canceled { t.Fatal("cancelled coordinator succeeded",ok,err) }
                } else if !ok || err!=nil { t.Fatal("complete coordinator failed",ok,err) }
                if !reflect.DeepEqual(events,expected) { t.Fatal("incorrect lifecycle order",events) }
            }
            if !cancelled && (owners[0]!=owners[2] || owners[1]!=owners[3] || pins[0]!=pins[2] || pins[1]!=pins[3]) {
                t.Fatal("retry changed task identity or canonical params")
            }
            for _,rows:=range opened { if !rows.closed { t.Fatal("source rows leaked") } }
        })
    }
}
