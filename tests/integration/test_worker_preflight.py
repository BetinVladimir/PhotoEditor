from test_task_leases import queued_task


def test_worker_preflight_bound_to_login_class_and_enabled_registration(db):
    x=queued_task(db)
    db.execute('UPDATE photo.resource_nodes SET enabled=true WHERE node_id=%s',(x['node'],))
    db.execute("UPDATE photo.worker_registrations SET certificate_sha256=%s WHERE worker_id=%s",('f'*64,x['worker']))
    db.execute('SET LOCAL ROLE photo_worker')
    assert db.execute('SELECT operation FROM photo.worker_runtime_operations(%s,%s)',(x['worker'],'media')).fetchall()==[('asset.verify',)]
    assert db.execute('SELECT operation FROM photo.worker_runtime_operations(%s,%s)',(x['worker'],'cpu')).fetchall()==[]
    db.execute('RESET ROLE')
    db.execute("UPDATE photo.worker_registrations SET db_principal='different_service' WHERE worker_id=%s",(x['worker'],))
    db.execute('SET LOCAL ROLE photo_worker')
    assert db.execute('SELECT operation FROM photo.worker_runtime_operations(%s,%s)',(x['worker'],'media')).fetchall()==[]


def test_cpu_heartbeat_refreshes_cpu_node_but_not_gpu_measurements(db):
    x=queued_task(db)
    db.execute('UPDATE photo.resource_nodes SET enabled=true WHERE node_id=%s',(x['node'],))
    db.execute("UPDATE photo.worker_registrations SET certificate_sha256=%s,last_seen=clock_timestamp()-interval '1 hour' WHERE worker_id=%s",('f'*64,x['worker']))
    db.execute('SET LOCAL ROLE photo_worker')
    args=(x['worker'],'media',['asset.verify'],999999999,999999999)
    assert db.execute('SELECT photo.heartbeat_cpu_worker(%s,%s,%s,%s,%s)',args).fetchone()[0]
    assert not db.execute('SELECT photo.heartbeat_cpu_worker(%s,%s,%s,%s,%s)',(x['worker'],'media',['asset.inspect'],100,100)).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT ram_available_bytes,scratch_available_bytes FROM photo.resource_nodes WHERE node_id=%s',(x['node'],)).fetchone()==(10000000,10000000)
    db.execute("UPDATE photo.resource_nodes SET vram_budget_bytes=1000,vram_available_bytes=777,reported_at=clock_timestamp()-interval '1 hour' WHERE node_id=%s",(x['node'],))
    before=db.execute('SELECT reported_at FROM photo.resource_nodes WHERE node_id=%s',(x['node'],)).fetchone()[0]
    db.execute('SET LOCAL ROLE photo_worker')
    assert db.execute('SELECT photo.heartbeat_cpu_worker(%s,%s,%s,%s,%s)',args).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT vram_available_bytes,reported_at FROM photo.resource_nodes WHERE node_id=%s',(x['node'],)).fetchone()==(777,before)


def test_accelerator_report_requires_exact_backend_and_qualified_node(db):
    x=queued_task(db)
    db.execute("UPDATE photo.resource_nodes SET enabled=true,vram_budget_bytes=1000,doctor_report_sha256=%s WHERE node_id=%s",('a'*64,x['node']))
    db.execute("UPDATE photo.worker_registrations SET resource_classes='[\"gpu\"]',backends='[\"mps\"]',qualified_operations='{\"pair.fit.color\":[\"mps\"]}',certificate_sha256=%s WHERE worker_id=%s",('f'*64,x['worker']))
    db.execute('SET LOCAL ROLE photo_worker')
    assert db.execute("SELECT operation FROM photo.worker_runtime_operations(%s,'gpu','mps')",(x['worker'],)).fetchall()==[('pair.fit.color',)]
    assert db.execute("SELECT operation FROM photo.worker_runtime_operations(%s,'gpu','cpu')",(x['worker'],)).fetchall()==[]
    args=(x['worker'],'mps',['pair.fit.color'],999999999,999999999,999999999)
    assert db.execute('SELECT photo.heartbeat_accelerator_worker(%s,%s,%s,%s,%s,%s)',args).fetchone()[0]
    assert not db.execute('SELECT photo.heartbeat_accelerator_worker(%s,%s,%s,%s,%s,%s)',(x['worker'],'cpu',['pair.fit.color'],999,0,999)).fetchone()[0]
    db.execute('RESET ROLE')
    assert db.execute('SELECT ram_available_bytes,vram_available_bytes,scratch_available_bytes FROM photo.resource_nodes WHERE node_id=%s',(x['node'],)).fetchone()==(10000000,1000,10000000)
    db.execute('UPDATE photo.resource_nodes SET doctor_report_sha256=NULL WHERE node_id=%s',(x['node'],))
    db.execute('SET LOCAL ROLE photo_worker')
    assert not db.execute('SELECT photo.heartbeat_accelerator_worker(%s,%s,%s,%s,%s,%s)',args).fetchone()[0]
