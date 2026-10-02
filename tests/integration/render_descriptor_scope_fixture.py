"""SQL authority checks over a materialized analysis; not GPU qualification."""
import json
from hashlib import sha256
from uuid import uuid4
from psycopg.types.json import Jsonb


def assert_render_descriptor_authority(db, tenant, project, version, source,
                                       run, analysis, params, actor, session, *, persist=False):
    with db.transaction(force_rollback=not persist):
        db.execute("UPDATE photo.artifacts SET output_role='working_image' WHERE id=%s", (source,))
        task = db.execute("INSERT INTO photo.tasks(tenant_id,run_id,node_key,operation,operation_version,resource_class,state,inputs,params_artifact_id,input_digest,attempt,fencing_token,execution_profile) VALUES(%s,%s,'session.describe:scope','session.describe','1','cpu','blocked',%s,%s,%s,1,1,%s) RETURNING id",
                          (tenant, run, Jsonb([]), params, '1'*64, Jsonb({'renderer_version':'v2.2'}))).fetchone()[0]
        descriptor = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,'test',%s,'v1',%s,64,'application/json',clock_timestamp(),%s,1,1,'document') RETURNING id",
                                (tenant, str(uuid4()), '7'*64, task)).fetchone()[0]
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'document','test',%s,%s,64,'application/json','ready','v1')",
                   (tenant,task,uuid4(),'4'*64,uuid4(),descriptor,str(uuid4()),'7'*64))
        result = db.execute("INSERT INTO photo.task_results(tenant_id,task_id,attempt,fencing_token,outputs,metrics,review_required,result_digest) VALUES(%s,%s,1,1,%s,%s,false,%s) RETURNING id",
                            (tenant, task, Jsonb([{'artifact_id':str(descriptor),'sha256':'7'*64}]), Jsonb({}), '8'*64)).fetchone()[0]
        db.execute("UPDATE photo.tasks SET state='succeeded',canonical_result_id=%s WHERE id=%s", (result, task))
        faces = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,'test',%s,'v1',%s,64,'application/json',clock_timestamp(),%s,1,1,'faces') RETURNING id",(tenant,str(uuid4()),'9'*64,task)).fetchone()[0]
        db.execute("INSERT INTO photo.output_uploads(tenant_id,task_id,attempt,fence,worker_id,certificate_sha256,idempotency_key,artifact_id,role,bucket,object_key,sha256,bytes,media_type,state,object_version) VALUES(%s,%s,1,1,%s,%s,%s,%s,'faces','test',%s,%s,64,'application/json','ready','v1')",(tenant,task,uuid4(),'4'*64,uuid4(),faces,str(uuid4()),'9'*64))
        db.execute("UPDATE photo.artifacts SET created_by_task_id=%s,produced_attempt=1,produced_fence=1 WHERE id=%s",(task,source))
        array = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,'test',%s,'v1',%s,64,'application/x-npy',clock_timestamp(),%s,1,1,'working_rgb') RETURNING id",(tenant,str(uuid4()),'6'*64,task)).fetchone()[0]
        alpha = db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,verified_at,created_by_task_id,produced_attempt,produced_fence,output_role) VALUES(%s,'test',%s,'v1',%s,64,'application/x-npy',clock_timestamp(),%s,1,1,'alpha') RETURNING id",(tenant,str(uuid4()),'5'*64,task)).fetchone()[0]
        binding = {'asset_version_id':str(version),'source':{'artifact_id':str(source),'sha256':'a'*64}}
        body = json.dumps({'schema_version':1,'recipe':{'source':binding},'recipe_sha256':'c'*64,'scalars':{'source':binding},'learning':None}, sort_keys=True,separators=(',',':'))
        digest = sha256(body.encode()).hexdigest()
        document = uuid4()
        db.execute("SELECT set_config('app.tenant_id',%s,true)",(str(tenant),))
        db.execute('SET LOCAL ROLE photo_api')
        args = (tenant,version,document,actor,session,digest,len(body.encode()),'test')
        upload = db.execute('SELECT * FROM photo.reserve_v2_draft_upload('+','.join(['%s']*8)+')',args).fetchone()
        assert upload[0]=='created'
        assert db.execute('SELECT photo.complete_v2_draft_upload('+','.join(['%s']*11)+')',(*args[:5],upload[1],upload[2],digest,args[6],args[7],'v1')).fetchone()==(True,)
        assert db.execute('SELECT photo.register_v26_render_recipe('+','.join(['%s']*10)+')',(*args[:5],upload[1],digest,source,'a'*64,body)).fetchone()==(True,)
        scope = 'SELECT photo.v26_render_descriptor_scope('+','.join(['%s']*12)+')'
        authority = (tenant,project,version,upload[1],digest,descriptor,'7'*64,analysis,0,'e'*64,actor,session)
        assert db.execute(scope,authority).fetchone()==(True,)
        source_scope = 'SELECT photo.v26_render_frame_source_scope('+','.join(['%s']*16)+')'
        source_authority = (*authority[:10],0,version,source,'a'*64,actor,session)
        assert db.execute(source_scope,source_authority).fetchone()==(True,)
        array_scope = 'SELECT photo.v26_render_frame_array_scope('+','.join(['%s']*19)+')'
        array_authority = (*source_authority[:14],array,'6'*64,'WorkingPixels',actor,session)
        assert db.execute(array_scope,array_authority).fetchone()==(True,)
        for index,replacement in ((14,uuid4()),(15,'f'*64),(16,'WorkingAlpha'),(16,'Artifact'),(10,1)):
            changed=list(array_authority); changed[index]=replacement
            assert db.execute(array_scope,changed).fetchone()==(False,)
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute('UPDATE photo.artifacts SET produced_fence=2 WHERE id=%s',(array,))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute(array_scope,array_authority).fetchone()==(False,)
        assert db.execute(array_scope,array_authority).fetchone()==(True,)
        faces_scope = 'SELECT photo.v26_render_frame_faces_scope('+','.join(['%s']*18)+')'
        faces_authority = (*source_authority[:14],faces,'9'*64,actor,session)
        assert db.execute(faces_scope,faces_authority).fetchone()==(True,)
        for index,replacement in ((14,uuid4()),(15,'f'*64),(10,1),(16,uuid4())):
            changed=list(faces_authority); changed[index]=replacement
            assert db.execute(faces_scope,changed).fetchone()==(False,)
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(faces,))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute(faces_scope,faces_authority).fetchone()==(False,)
        assert db.execute(faces_scope,faces_authority).fetchone()==(True,)
        for index, replacement in ((10,1),(10,-1),(10,5000),(11,uuid4()),(12,uuid4()),(13,'f'*64),(14,uuid4()),(15,uuid4())):
            changed = list(source_authority); changed[index] = replacement
            assert db.execute(source_scope,changed).fetchone()==(False,)
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.asset_versions SET review_state='pending' WHERE id=%s",(version,))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute(source_scope,source_authority).fetchone()==(False,)
        assert db.execute(source_scope,source_authority).fetchone()==(True,)
        for index, replacement in ((2,uuid4()),(5,uuid4()),(6,'f'*64),(7,uuid4()),(8,1),(9,'f'*64),(10,uuid4()),(11,uuid4())):
            changed = list(authority); changed[index] = replacement
            assert db.execute(scope,changed).fetchone()==(False,)
        for statement, values in (
            ("UPDATE photo.tasks SET fencing_token=2 WHERE id=%s",(task,)),
            ("UPDATE photo.tasks SET state='blocked' WHERE id=%s",(task,)),
            ("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE id=%s",(descriptor,)),
            ("UPDATE photo.runs SET cancel_requested=true WHERE id=%s",(run,)),
        ):
            with db.transaction(force_rollback=True):
                db.execute('RESET ROLE'); db.execute(statement,values)
                db.execute('SET LOCAL ROLE photo_api')
                assert db.execute(scope,authority).fetchone()==(False,)
            assert db.execute(scope,authority).fetchone()==(True,)
        db.execute('RESET ROLE')
    return {'Tenant':str(tenant),'Project':str(project),'Version':str(version),
            'Root':{'artifact_id':str(upload[1]),'sha256':digest},
            'Descriptor':{'artifact_id':str(descriptor),'sha256':'7'*64},
            'Source':{'artifact_id':str(source),'sha256':'a'*64},
            'Faces':{'artifact_id':str(faces),'sha256':'9'*64},
            'Pixels':{'artifact_id':str(array),'sha256':'6'*64},
            'Alpha':{'artifact_id':str(alpha),'sha256':'5'*64},
            'Analysis':str(analysis),'Actor':str(actor),'Session':str(session)}
