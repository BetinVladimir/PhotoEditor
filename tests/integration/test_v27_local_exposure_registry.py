def test_local_exposure_catalog_is_cpu_and_requires_explicit_activation(db):
    row=db.execute("SELECT resource_class,routing_key,timeout_seconds,backends,v2_enabled FROM photo.operation_registry WHERE operation='semantic.local_exposure' AND version='1'").fetchone()
    assert row==('cpu','cpu.semantic_local_exposure',1800,['cpu'],False)
    rows=db.execute("SELECT role,media_types,max_bytes,max_count FROM photo.operation_output_policies WHERE operation='semantic.local_exposure' AND version='1' ORDER BY role").fetchall()
    assert rows==[
        ('document',['application/json'],8388608,1),
        ('pixels',['application/x-npy'],2400016384,1),
        ('report',['application/json'],8388608,1),
        ('working_image',['application/json'],8388608,1),
    ]
