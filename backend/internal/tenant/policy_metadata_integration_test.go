package tenant

import (
 "context"
 "encoding/json"
 "testing"
)

func TestIntegrationPolicyEditPreservesStoredMetadata(t *testing.T) {
 s, admin, who, tid, _ := tenantFixture(t)
 ctx := context.Background()
 _, err := admin.Exec(ctx, `UPDATE photo.tenants SET
  quotas=quotas||'{"active_gpu_tasks":7,"custom_quota_note":{"owner":"original"}}',
  retention=retention||'{"deleted_days":91,"artifact_days":83,"custom_retention_note":"original"}' WHERE id=$1`,tid)
 if err != nil { t.Fatal(err) }
 quotas := Quotas{StorageBytes:23456789,SourceFiles:1234,MaxActiveGPUTasks:3}
 retention := Retention{ProcessingDays:19,ExportsDays:42,TemporaryHours:48}
 out, err := s.Update(ctx,who,tid,1,PatchRequest{Quotas:&quotas,Retention:&retention})
 if err != nil { t.Fatal(err) }
 var q,r map[string]any
 if err=json.Unmarshal(out.Quotas,&q);err!=nil {t.Fatal(err)}
 if err=json.Unmarshal(out.Retention,&r);err!=nil {t.Fatal(err)}
 note,ok:=q["custom_quota_note"].(map[string]any)
 if q["max_active_gpu_tasks"]!=float64(3)||q["active_gpu_tasks"]!=float64(7)||!ok||note["owner"]!="original" {t.Fatalf("quota metadata changed: %v",q)}
 if r["processing_days"]!=float64(19)||r["deleted_days"]!=float64(91)||r["artifact_days"]!=float64(83)||r["custom_retention_note"]!="original" {t.Fatalf("retention metadata changed: %v",r)}
 if out.Revision!="2" {t.Fatalf("revision=%s",out.Revision)}
}
