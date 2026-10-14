"""SQL metadata authority fixture; no pixel/model qualification claim."""
from uuid import uuid4


def assert_registered_render_model_authority(db,tenant,project,binding,learning,actor,session,consent):
    with db.transaction(force_rollback=True):
        db.execute('RESET ROLE')
        source=binding['source']
        db.execute("UPDATE photo.artifacts SET output_role='working_image' WHERE tenant_id=%s AND id=%s",(tenant,source['artifact_id']))
        root=db.execute("INSERT INTO photo.artifacts(tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role,verified_at) VALUES(%s,'test',%s,'v1',%s,64,'application/json','document',now()) RETURNING id",(tenant,str(uuid4()),'9'*64)).fetchone()[0]
        db.execute("INSERT INTO photo.v2_draft_uploads(tenant_id,document_id,project_id,asset_version_id,artifact_id,sha256,bytes,bucket,state,object_version,completed_at,render_source_id,render_source_sha,render_registered_at,render_authored_sha) VALUES(%s,%s,%s,%s,%s,%s,64,'test','ready','v1',now(),%s,%s,now(),%s)",(tenant,uuid4(),project,binding['asset_version_id'],root,'9'*64,source['artifact_id'],source['sha256'],'2'*64))
        model,model_sha=db.execute('SELECT a.id,a.sha256 FROM photo.learning_versions v JOIN photo.artifacts a ON a.tenant_id=v.tenant_id AND a.id=v.model_artifact_id WHERE v.tenant_id=%s AND v.id=%s',(tenant,learning)).fetchone()
        args=(tenant,project,binding['asset_version_id'],root,'9'*64,model,model_sha,learning,actor,session)
        query='SELECT photo.v26_render_model_scope('+','.join(['%s']*10)+')'
        consent_query='SELECT photo.v26_render_model_consent_revoked('+','.join(['%s']*10)+')'
        # The diagnostic orders learning before model, unlike model_scope.
        consent_args=(*args[:5],learning,model,model_sha,actor,session)
        db.execute('SET LOCAL ROLE photo_api')
        actual=db.execute(query,args).fetchone()
        if actual!=(True,):
            db.execute('RESET ROLE')
            diagnostic=db.execute('SELECT e.status,p.active_version_id=v.id,m.output_role,t.output_role,m.state,t.state,photo.v23_learning_version_consents_live(v.tenant_id,v.id),photo.v26_render_recipe_scope(%s,%s,%s,%s,%s,%s,%s) FROM photo.learning_versions v JOIN photo.learning_profiles p ON p.tenant_id=v.tenant_id AND p.id=v.profile_id JOIN photo.version_eligibility e ON e.tenant_id=v.tenant_id AND e.version_id=v.id JOIN photo.artifacts m ON m.tenant_id=v.tenant_id AND m.id=v.model_artifact_id JOIN photo.artifacts t ON t.tenant_id=v.tenant_id AND t.id=v.training_artifact_id WHERE v.tenant_id=%s AND v.id=%s',(tenant,project,binding['asset_version_id'],root,'9'*64,actor,session,tenant,learning)).fetchone()
            raise AssertionError(diagnostic)
        for index,replacement in ((5,uuid4()),(6,'f'*64),(7,uuid4())):
            wrong=list(args);wrong[index]=replacement
            assert db.execute(query,wrong).fetchone()==(False,)
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute('UPDATE photo.v2_draft_uploads SET render_authored_sha=%s WHERE tenant_id=%s AND artifact_id=%s',('f'*64,tenant,root))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute(query,args).fetchone()==(False,)
        assert db.execute(query,args).fetchone()==(True,)
        assert db.execute(consent_query,consent_args).fetchone()==(False,)
        with db.transaction(force_rollback=True):
            db.execute('RESET ROLE')
            db.execute("UPDATE photo.consent_grants SET status='revoked',epoch=epoch+1 WHERE tenant_id=%s AND id=%s",(tenant,consent))
            db.execute('SET LOCAL ROLE photo_api')
            assert db.execute(query,args).fetchone()==(False,)
            assert db.execute(consent_query,consent_args).fetchone()==(True,)
            # A revoked grant must not become an oracle for guessed foreign
            # roots, model/version identifiers or actor/session credentials.
            for index,replacement in ((0,uuid4()),(1,uuid4()),(2,uuid4()),(3,uuid4()),
                    (4,'f'*64),(5,uuid4()),(6,uuid4()),(7,'f'*64),(8,uuid4()),(9,uuid4())):
                wrong=list(consent_args);wrong[index]=replacement
                assert db.execute(consent_query,wrong).fetchone()==(False,)
            with db.transaction(force_rollback=True):
                db.execute('RESET ROLE')
                db.execute('UPDATE photo.v2_draft_uploads SET render_authored_sha=%s WHERE tenant_id=%s AND artifact_id=%s',
                    ('f'*64,tenant,root))
                db.execute('SET LOCAL ROLE photo_api')
                assert db.execute(consent_query,consent_args).fetchone()==(False,)
            assert db.execute(consent_query,consent_args).fetchone()==(True,)
        # Revocation in this SQL fixture is rolled back; this is metadata
        # authority coverage, not evidence of an executed HTTP revoke probe.
        assert db.execute(consent_query,consent_args).fetchone()==(False,)
