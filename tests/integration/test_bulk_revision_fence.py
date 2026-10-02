from uuid import uuid4

from psycopg.types.json import Jsonb


def test_bulk_keeps_completed_chunk_and_reports_revision_conflict_for_remainder(db):
    digest = "c" * 64
    user = db.execute(
        "SELECT user_id FROM photo.auth_bootstrap_user(%s,'en',true)",
        (f"{uuid4()}@test.example",),
    ).fetchone()[0]
    tenant = db.execute(
        "SELECT id FROM photo.tenants WHERE owner_user_id=%s", (user,)
    ).fetchone()[0]
    project = db.execute(
        "INSERT INTO photo.projects(tenant_id,name,visibility) VALUES(%s,'Bulk','workspace') RETURNING id",
        (tenant,),
    ).fetchone()[0]
    artifact = db.execute(
        """INSERT INTO photo.artifacts
           (tenant_id,bucket,object_key,object_version,sha256,bytes,media_type,output_role)
           VALUES(%s,'photo',%s,'v1',%s,1,'image/jpeg','working_image') RETURNING id""",
        (tenant, f"bulk/{uuid4()}", digest),
    ).fetchone()[0]
    collection = db.execute(
        "INSERT INTO photo.collections(tenant_id,project_id,name,state) VALUES(%s,%s,'Bulk source','draft') RETURNING id",
        (tenant, project),
    ).fetchone()[0]

    assets = [uuid4() for _ in range(501)]
    versions = [uuid4() for _ in range(501)]
    items = [uuid4() for _ in range(501)]
    with db.cursor() as cursor:
        cursor.executemany(
            """INSERT INTO photo.assets
               (id,tenant_id,project_id,original_artifact_id,metadata,upload_state,provenance)
               VALUES(%s,%s,%s,%s,'{}','verified','{}')""",
            ((assets[i], tenant, project, artifact) for i in range(501)),
        )
        cursor.executemany(
            """INSERT INTO photo.asset_versions
               (id,tenant_id,asset_id,master_artifact_id,content_hash,metadata,review_state)
               VALUES(%s,%s,%s,%s,%s,'{}','accepted')""",
            ((versions[i], tenant, assets[i], artifact, digest) for i in range(501)),
        )
        cursor.executemany(
            """INSERT INTO photo.collection_items
               (id,tenant_id,collection_id,asset_id,asset_version_id,position)
               VALUES(%s,%s,%s,%s,%s,%s)""",
            ((items[i], tenant, collection, assets[i], versions[i], i) for i in range(501)),
        )

    snapshot = db.execute(
        """INSERT INTO photo.snapshots
           (tenant_id,kind,collection_id,source_revision,principal_type,principal_id,digest,item_count)
           VALUES(%s,'selection',%s,1,'user',%s,%s,501) RETURNING id""",
        (tenant, collection, user, digest),
    ).fetchone()[0]
    with db.cursor() as cursor:
        cursor.executemany(
            """INSERT INTO photo.snapshot_items
               (tenant_id,snapshot_id,ordinal,item_id,asset_version_id,artifact_id,sha256,payload)
               VALUES(%s,%s,%s,%s,%s,%s,%s,%s)""",
            (
                (tenant, snapshot, i, items[i], versions[i], artifact, digest, Jsonb({}))
                for i in range(501)
            ),
        )

    run = db.execute(
        """INSERT INTO photo.runs
           (tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by,graph_sealed)
           VALUES(%s,'bulk','queued',%s,'{}',%s,%s,false) RETURNING id""",
        (tenant, snapshot, "d" * 64, user),
    ).fetchone()[0]
    bulk = db.execute(
        """INSERT INTO photo.bulk_operations
           (tenant_id,collection_id,snapshot_id,action,params,expected_next_revision,status,created_by,run_id)
           VALUES(%s,%s,%s,'rating',%s,1,'queued',%s,%s) RETURNING id""",
        (tenant, collection, snapshot, Jsonb({"rating": 5}), user, run),
    ).fetchone()[0]

    assert db.execute("SELECT photo.enqueue_bulk_chunk()").fetchone()[0] is True
    first_event = db.execute(
        "SELECT pending_event_id FROM photo.bulk_operations WHERE id=%s", (bulk,)
    ).fetchone()[0]
    assert db.execute(
        "SELECT photo.process_bulk_chunk(%s,%s)", (bulk, first_event)
    ).fetchone()[0] is True

    first_state = db.execute(
        """SELECT b.status,b.next_ordinal,b.expected_next_revision,c.revision,
                  count(*) FILTER (WHERE r.status='succeeded')
           FROM photo.bulk_operations b
           JOIN photo.collections c ON c.id=b.collection_id
           LEFT JOIN photo.bulk_item_results r ON r.bulk_operation_id=b.id
           WHERE b.id=%s GROUP BY b.status,b.next_ordinal,b.expected_next_revision,c.revision""",
        (bulk,),
    ).fetchone()
    assert first_state == ("running", 500, 2, 2, 500)
    assert db.execute(
        "SELECT count(*) FROM photo.collection_items WHERE collection_id=%s AND rating=5",
        (collection,),
    ).fetchone()[0] == 500

    db.execute("UPDATE photo.collections SET revision=revision+1 WHERE id=%s", (collection,))
    assert db.execute("SELECT photo.enqueue_bulk_chunk()").fetchone()[0] is True
    second_event = db.execute(
        "SELECT pending_event_id FROM photo.bulk_operations WHERE id=%s", (bulk,)
    ).fetchone()[0]
    assert db.execute(
        "SELECT photo.process_bulk_chunk(%s,%s)", (bulk, second_event)
    ).fetchone()[0] is True

    final_state = db.execute(
        """SELECT b.status,b.next_ordinal,b.pending_event_id,r.status,r.graph_sealed
           FROM photo.bulk_operations b JOIN photo.runs r ON r.id=b.run_id WHERE b.id=%s""",
        (bulk,),
    ).fetchone()
    assert final_state == ("completed_with_errors", 500, None, "completed_with_errors", True)
    report = db.execute(
        """SELECT status,error_code,count(*) FROM photo.bulk_item_results
           WHERE bulk_operation_id=%s GROUP BY status,error_code ORDER BY status""",
        (bulk,),
    ).fetchall()
    assert report == [("failed", "REVISION_CONFLICT", 1), ("succeeded", None, 500)]
    assert db.execute(
        "SELECT rating FROM photo.collection_items WHERE id=%s", (items[500],)
    ).fetchone()[0] == 0
