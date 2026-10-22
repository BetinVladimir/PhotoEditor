"""Current SQL adopter rollback contract; authored, not runtime qualification."""
from uuid import uuid4
import pytest
from psycopg.types.json import Jsonb
from test_apply_v4_plan import _build


def _adoptable(db, graph_version, admit=None, count=2, build=None):
    # Both graph versions deliberately share this public adoption boundary.
    tenant, run, token, items = _build(db,count=count) if build is None else build(db)
    project, source = db.execute(
        "SELECT c.project_id,c.id FROM photo.collections c JOIN photo.apply_builds b "
        "ON b.source_collection_id=c.id WHERE b.run_id=%s", (run,)
    ).fetchone()
    output = db.execute(
        "INSERT INTO photo.collections(tenant_id,project_id,name,state) "
        "VALUES(%s,%s,'output','draft') RETURNING id", (tenant, project)
    ).fetchone()[0]
    db.execute("UPDATE photo.runs SET output_collection_id=%s,graph_sealed=true WHERE id=%s", (output, run))
    db.execute("UPDATE photo.apply_builds SET phase='adopt',graph_version=%s,stage_index=%s WHERE run_id=%s", (graph_version,1 if graph_version==4 else None,run))
    if graph_version == 4:
        for index,phase,operation,role in [(0,'local','image.local','working_image'),(1,'quality','quality.check','quality_report')]:
            db.execute("INSERT INTO photo.apply_v4_stages(tenant_id,run_id,stage_index,phase,operation,output_role,cardinality,plan_hash) VALUES(%s,%s,%s,%s,%s,%s,'item',%s)", (tenant,run,index,phase,operation,role,'a'*64))
    if admit is not None:
        db.execute("UPDATE photo.runs SET graph_sealed=false WHERE id=%s",(run,))
        db.execute("UPDATE photo.apply_builds SET phase='resolve' WHERE run_id=%s",(run,))
        admit(db, tenant, run, token, items, output)
        db.execute("UPDATE photo.runs SET graph_sealed=true WHERE id=%s",(run,))
        db.execute("UPDATE photo.apply_builds SET phase='adopt' WHERE run_id=%s",(run,))
    profile = Jsonb({'backend':'cpu','precision':'float32','tile_size':512,
                    'resource_estimate':{'ram_bytes':1,'vram_bytes':0,'scratch_bytes':1,'output_bytes':1,'pixel_count':1}})
    for ordinal, (item, asset, version, image, image_sha) in enumerate(items):
        params = db.execute(
            "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role) "
            "VALUES(%s,'photo-private',%s,'v1',%s,2,'application/json','ready','task_params') RETURNING id",
            (tenant, f'params/{uuid4()}', 'f'*64)
        ).fetchone()[0]
        local_ref = None
        for phase, operation, role in [('local','image.local','working_image'), ('quality','quality.check','quality_report')]:
            task = uuid4()
            key = f'{operation}/{ordinal:04d}'
            target = local_ref if phase=='quality' else {'artifact_id':str(image),'sha256':image_sha}
            inputs = Jsonb([target])
            node_params = {'target':target,'target_kind':'working','stage':'master'} if phase=='quality' else {}
            db.execute(
                "INSERT INTO photo.apply_nodes(id,tenant_id,run_id,phase,ordinal,source_item_id,source_asset_id,source_version_id,node_key,operation,inputs,params_sha256,params) "
                "VALUES(%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s,%s)",
                (task,tenant,run,phase,ordinal,item,asset,version,key,operation,inputs,'f'*64,Jsonb(node_params))
            )
            db.execute(
                "INSERT INTO photo.tasks(id,tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,execution_profile) "
                "VALUES(%s,%s,%s,%s,%s,'1','cpu','blocked',%s,%s,%s,%s)",
                (task,tenant,run,key,operation,inputs,params,'e'*64,profile)
            )
            digest = uuid4().hex*2
            artifact = db.execute(
                "INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,state,output_role,created_by_task_id,produced_attempt,produced_fence) "
                "VALUES(%s,'photo-private',%s,'v1',%s,128,'application/json','ready',%s,%s,0,0) RETURNING id",
                (tenant,f'outputs/{uuid4()}',digest,role,task)
            ).fetchone()[0]
            if phase == 'local':
                local_ref = {'artifact_id':str(artifact),'sha256':digest}
            result = db.execute(
                "INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,result_digest) "
                "VALUES(%s,%s,0,0,%s,'{}',%s) RETURNING id",
                (tenant,task,Jsonb([{'artifact_id':str(artifact),'sha256':digest}]),uuid4().hex*2)
            ).fetchone()[0]
            db.execute("INSERT INTO photo.task_attempts(tenant_id,task_id,attempt,fencing_token,worker_id,started_at,finished_at,backend,profile) VALUES(%s,%s,0,0,%s,clock_timestamp(),clock_timestamp(),'cpu','{}')",(tenant,task,uuid4()))
            db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s", (result,task))
    return tenant, run, token, items, output


def _state(db, tenant, run, output):
    return (
        db.execute("SELECT count(*) FROM photo.asset_versions WHERE tenant_id=%s AND created_by_run_id=%s",(tenant,run)).fetchone(),
        db.execute("SELECT count(*) FROM photo.asset_version_sources s JOIN photo.asset_versions v ON v.tenant_id=s.tenant_id AND v.id=s.version_id WHERE v.tenant_id=%s AND v.created_by_run_id=%s",(tenant,run)).fetchone(),
        db.execute("SELECT count(*) FROM photo.collection_items WHERE tenant_id=%s AND collection_id=%s",(tenant,output)).fetchone(),
        db.execute("SELECT status,revision FROM photo.runs WHERE id=%s",(run,)).fetchone(),
        db.execute("SELECT phase,lease_token,lease_until FROM photo.apply_builds WHERE run_id=%s",(run,)).fetchone(),
        db.execute("SELECT revision FROM photo.collections WHERE id=%s",(output,)).fetchone(),
        db.execute("SELECT reserved_bytes,storage_bytes FROM photo.tenant_usage WHERE tenant_id=%s",(tenant,)).fetchone(),
        db.execute("SELECT id,bytes,state FROM photo.quota_reservations WHERE tenant_id=%s AND holder_type='run' AND holder_id=%s ORDER BY id",(tenant,run)).fetchall(),
    )


@pytest.mark.parametrize("graph_version", [3, 4])
def test_apply_refusal_after_first_item_rolls_back_and_retry_does_not_duplicate(db, graph_version):
    tenant, run, token, items, output = _adoptable(db,graph_version)
    # First item is fully adoptable. The second fails only after the first item's
    # version, source lineage and output membership have already been inserted.
    db.execute("UPDATE photo.assets SET deleted_at=clock_timestamp() WHERE id=%s", (items[1][1],))
    before = _state(db,tenant,run,output)
    for _ in range(2):
        assert db.execute("SELECT photo.adopt_apply_results(%s,%s,%s)",(tenant,run,token)).fetchone()[0] is False
        assert _state(db,tenant,run,output) == before
    db.execute("UPDATE photo.assets SET deleted_at=NULL WHERE id=%s", (items[1][1],))
    assert db.execute("SELECT photo.adopt_apply_results(%s,%s,%s)",(tenant,run,token)).fetchone()[0] is True
    after = _state(db,tenant,run,output)
    assert after[0] == (2,)
    assert after[1] == (2,)
    assert after[2] == (2,)
    assert after[3][0] == 'completed'
    assert after[4][0] == 'complete'
    assert db.execute("SELECT photo.adopt_apply_results(%s,%s,%s)",(tenant,run,token)).fetchone()[0] is False
    assert _state(db,tenant,run,output) == after


def test_apply_previous_adopter_has_no_scheduler_or_public_bypass(db):
    assert db.execute("SELECT has_function_privilege('photo_scheduler','photo.adopt_apply_results(uuid,uuid,uuid)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT has_function_privilege('photo_scheduler','photo.adopt_apply_results_before_atomic_rejection(uuid,uuid,uuid)','EXECUTE')").fetchone()[0]
    assert not db.execute("SELECT EXISTS(SELECT FROM pg_proc p CROSS JOIN LATERAL aclexplode(p.proacl) a WHERE p.oid='photo.adopt_apply_results_before_atomic_rejection(uuid,uuid,uuid)'::regprocedure AND a.grantee=0 AND a.privilege_type='EXECUTE')").fetchone()[0]
