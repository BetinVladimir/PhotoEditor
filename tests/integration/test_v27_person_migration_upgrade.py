"""Real 00899→00973 database upgrade; metadata preservation, not image quality."""
from pathlib import Path
import json
from uuid import UUID, uuid4, uuid5

import pytest
from psycopg import errors, sql
from psycopg.types.json import Jsonb

from test_v27_native_anchor import seed_native_anchor

ROOT = Path(__file__).resolve().parents[2]
ROLES = ('photo_api', 'photo_scheduler', 'photo_worker', 'photo_publisher',
         'photo_portal', 'photo_auth', 'photo_gateway', 'photo_maintenance')


def function_inventory(db):
    return {row[0]: row[1:] for row in db.execute("""
        SELECT p.oid::regprocedure::text, p.oid, p.proowner, p.proacl::text,
               p.prosecdef, p.proconfig, md5(pg_get_functiondef(p.oid))
        FROM pg_proc p JOIN pg_namespace n ON n.oid=p.pronamespace
        WHERE n.nspname='photo' AND p.prokind='f'
        ORDER BY 1
    """).fetchall()}


def tenant_inventory(db, tenant):
    tables = db.execute("""
        SELECT c.relname FROM pg_class c JOIN pg_namespace n ON n.oid=c.relnamespace
        JOIN pg_attribute a ON a.attrelid=c.oid AND a.attname='tenant_id'
        WHERE n.nspname='photo' AND c.relkind='r' AND NOT a.attisdropped
        ORDER BY c.relname
    """).fetchall()
    return {name: db.execute(sql.SQL("""
        SELECT coalesce(jsonb_agg((to_jsonb(t)-'v27_target_manifest'-'v27_target_manifest_sha') ORDER BY to_jsonb(t)::text),'[]'::jsonb)
        FROM photo.{} t WHERE tenant_id=%s
    """).format(sql.Identifier(name)), (tenant,)).fetchone()[0] for name, in tables}


@pytest.mark.migration_head(899)
def test_person_forward_upgrade_preserves_existing_work(db):
    assert db.execute("SELECT to_regprocedure('photo.claim_v27_person_admission(uuid,integer)')").fetchone()[0] is None
    packet, case, _ = seed_native_anchor(db, 'mask.person', 'person')
    tenant, actor = case['Tenant'], case['Actor']
    person = UUID(packet['run_id'])
    # Old identity-free queued checkpoint must remain byte-for-byte unchanged;
    # migration cannot manufacture qualification pins or enable recovery.
    db.execute("""INSERT INTO photo.runs
        (id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy)
        VALUES(%s,%s,'v2',%s,%s,%s,%s,'queued',true,'fail_fast')""",
        (person, tenant, case['Snapshot'], Jsonb(json.loads(packet['config_text'])), 'c'*64, actor))
    db.execute("""INSERT INTO photo.tasks
        (id,tenant_id,run_id,node_key,operation,operation_version,resource_class,inputs,params_artifact_id,input_digest,execution_profile)
        VALUES(%s,%s,%s,'mask.person:0','mask.person','1','gpu','[]',%s,%s,%s)""",
        (uuid5(person, 'legacy-task'), tenant, person, packet['registration']['params']['artifact_id'],
         'd'*64, Jsonb(dict(backend='cuda', renderer_version='v2.2'))))
    apply_run = uuid4()
    db.execute("""INSERT INTO photo.runs
        (id,tenant_id,kind,input_snapshot_id,config_snapshot,config_hash,created_by,status,graph_sealed,failure_policy)
        VALUES(%s,%s,'apply',%s,%s,%s,%s,'running',true,'fail_fast')""",
        (apply_run, tenant, case['Snapshot'], Jsonb(dict(project_id=case['Project'], approved_semantic={})), 'e'*64, actor))
    db.execute('SET CONSTRAINTS ALL IMMEDIATE')  # flush fixture deferred triggers before DDL
    before_rows, before_functions = tenant_inventory(db, tenant), function_inventory(db)
    migrations = sorted(p for p in (ROOT/'backend/db/migrations').glob('*.sql') if 900 <= int(p.name[:5]) <= 973)
    assert len(migrations) == 74
    for migration in migrations:
        previous = function_inventory(db) if migration.name.startswith(('00921_','00929_','00938_','00948_','00951_','00954_','00955_','00969_','00970_','00971_','00972_','00973_')) else None
        db.execute(migration.read_text().split('-- +goose Down')[0])
        assert tenant_inventory(db, tenant) == before_rows, migration.name
        if previous is not None:
            current=function_inventory(db)
            for signature, old in previous.items():
                assert current[signature][:-1]==old[:-1],signature
                if signature.split('(')[0] not in {'photo.v27_native_initial_terminal_state','photo.record_v27_native_initial_pipeline','photo.lock_v27_target_defect_sealed_graph_scope','photo.semantic_target_defect_sealed_registration_state','photo.lock_semantic_target_defect_sealed_evidence','photo.claim_task','photo.guard_v2_result_root','photo.v2_pending_shell_matches','photo.lock_v27_target_skin_sealed_graph_scope','photo.semantic_target_skin_sealed_registration_state','photo.lock_semantic_target_skin_sealed_evidence','photo.semantic_target_skin_result_catalog','photo.lock_v27_target_face_sealed_graph_scope','photo.semantic_target_face_sealed_registration_state','photo.lock_semantic_target_face_sealed_evidence','photo.semantic_target_face_canonical_outputs','photo.lock_semantic_target_face_result','photo.lock_v27_target_detector_sealed_graph_scope','photo.semantic_target_detector_sealed_registration_state','photo.lock_semantic_target_detector_sealed_evidence','photo.semantic_target_detector_canonical_outputs','photo.lock_semantic_target_detector_result','photo.lock_v27_target_person_sealed_graph_scope','photo.semantic_target_person_sealed_registration_state','photo.lock_semantic_target_person_sealed_evidence','photo.semantic_target_person_canonical_outputs','photo.lock_semantic_target_person_result'}:
                    assert current[signature]==old,signature

    assert db.execute('SELECT count(*) FROM photo.runs WHERE tenant_id=%s AND (v27_target_manifest IS NOT NULL OR v27_target_manifest_sha IS NOT NULL)', (tenant,)).fetchone()[0] == 0
    after_functions = function_inventory(db)
    replacements = {'photo.record_v27_native_initial_pipeline(uuid,uuid,uuid,text)','photo.lock_v27_target_face_sealed_graph_scope(uuid,uuid,uuid)','photo.semantic_target_face_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)','photo.lock_semantic_target_face_sealed_evidence(uuid,uuid,jsonb)','photo.semantic_target_face_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text)','photo.lock_semantic_target_face_result(uuid,uuid,jsonb,uuid,jsonb)','photo.semantic_proposal_face_parse_preparation_pixels(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,uuid,text)','photo.lock_v27_target_detector_sealed_graph_scope(uuid,uuid,uuid)', 'photo.semantic_target_detector_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)', 'photo.lock_semantic_target_detector_sealed_evidence(uuid,uuid,jsonb)', 'photo.semantic_target_detector_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text)', 'photo.lock_semantic_target_detector_result(uuid,uuid,jsonb,uuid,jsonb)','photo.lock_v27_target_person_sealed_graph_scope(uuid,uuid,uuid)',
                    'photo.semantic_target_person_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)',
                    'photo.lock_semantic_target_person_sealed_evidence(uuid,uuid,jsonb)',
                    'photo.semantic_target_person_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text)',
                    'photo.lock_semantic_target_person_result(uuid,uuid,jsonb,uuid,jsonb)','photo.guard_v2_result_root()','photo.v2_pending_shell_matches(uuid,uuid,uuid,uuid,uuid,uuid,text,text,text,jsonb,jsonb,jsonb,bigint)',
                    'photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text)',
                    'photo.reserve_dispatch_resources(uuid,text,bigint,bigint,bigint,uuid,uuid,text)',
                    'photo.claim_task(uuid,integer,uuid,jsonb)',
                    'photo.lock_semantic_native_defect_admission_evidence(uuid,uuid,jsonb)',
                    'photo.lock_semantic_native_defect_completed_evidence(uuid,uuid,jsonb)',
                    'photo.seal_semantic_native_defect_preparation(uuid,uuid,jsonb)'}
    for signature, old in before_functions.items():
        new = after_functions[signature]
        assert new[:-1] == old[:-1], signature  # OID, owner, ACL and security properties
        if signature not in replacements:
            assert new == old, signature
        else:
            assert new[-1] != old[-1], signature
    expected_new = {
        'photo.v27_native_initial_terminal_state(uuid,uuid,uuid,uuid,uuid)': 'photo_api',
        'photo.lock_semantic_native_targets_transfer_repair_state_v3(uuid,uuid,uuid,uuid,uuid,text,text)': 'photo_scheduler',
        'photo.lock_semantic_native_targets_transfer_repair_state_v2(uuid,uuid,uuid,uuid,uuid,text,text)': 'photo_scheduler',
        'photo.lock_semantic_native_targets_transfer_repair_state(uuid,uuid,uuid,uuid,uuid,text,text)': 'photo_scheduler',
        'photo.lock_v27_native_handoff_qualification_chain(uuid,uuid,uuid,uuid,uuid,uuid,uuid,text)': 'no_application_role',
        'photo.record_v27_native_initial_pipeline_handoff(uuid,uuid,uuid,text,text)': 'photo_scheduler',
        'photo.semantic_native_target_checkpoint(uuid,uuid,uuid,uuid,uuid,text,uuid)': 'photo_api',
        'photo.semantic_native_targets_transfer_saved_config(uuid,uuid,uuid,uuid,uuid,text,uuid)': 'photo_api',
        'photo.lock_semantic_native_completed_person_authority(uuid,uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb,jsonb)': 'no_application_role',
        'photo.lock_semantic_native_original_defect_completed_evidence(uuid,uuid,jsonb)': 'no_application_role',
        'photo.lock_semantic_native_original_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_semantic_native_original_protection_evidence(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,jsonb,uuid,text,jsonb,jsonb)': 'photo_scheduler',
        'photo.lock_semantic_target_defect_protection(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,jsonb)': 'photo_scheduler',
        'photo.register_semantic_target_defect_protection(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb,uuid,uuid,text,text,text)': 'photo_scheduler',
        'photo.semantic_target_defect_protection_object(uuid,uuid,uuid,uuid,uuid,text,uuid)': 'photo_api',
        'photo.semantic_target_defect_saved(uuid,uuid,uuid,uuid,uuid,integer,text,text)': 'photo_api',
        'photo.semantic_target_defect_registered_transport(uuid,uuid,uuid,uuid,uuid)': 'photo_scheduler',
        'photo.lock_semantic_target_defect_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_defect_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid)': 'photo_api',
        'photo.semantic_target_defect_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid)': 'photo_api',
        'photo.semantic_target_defect_completed_output_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.finish_semantic_target_defect(uuid,uuid,jsonb,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_defect_result_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.lock_v27_target_defect_sealed_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_defect_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'no_application_role',
        'photo.semantic_target_defect_sealed_registration_binding_state(uuid,uuid,text,jsonb)': 'no_application_role',
        'photo.lock_semantic_target_defect_sealed_evidence(uuid,uuid,jsonb)': 'no_application_role',
        'photo.seal_semantic_target_defect_preparation(uuid,uuid,jsonb)': 'photo_scheduler', 
        'photo.lock_semantic_target_defect_graph_budget(uuid,uuid,jsonb)': 'no_application_role',
        'photo.lock_semantic_target_defect_admission_evidence(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_v27_target_defect_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_defect_preparation_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'no_application_role',
        'photo.semantic_target_defect_registration_binding_state(uuid,uuid,text,jsonb)': 'photo_scheduler',
        'photo.lock_semantic_target_defect_completed_dependencies(uuid,uuid,uuid,uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_skin_saved(uuid,uuid,uuid,uuid,uuid,integer,text,text)': 'photo_api',
        'photo.semantic_target_skin_registered_transport(uuid,uuid,uuid,uuid,uuid)': 'photo_scheduler',
        'photo.semantic_target_skin_completed_output_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.finish_semantic_target_skin(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_semantic_target_skin_result(uuid,uuid,uuid,uuid,uuid,text,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_skin_result_catalog(uuid,uuid,uuid,uuid,uuid,text,uuid)': 'photo_api',
        'photo.semantic_target_skin_result_objects(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid)': 'photo_api',
        'photo.semantic_target_skin_result_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.lock_v27_target_skin_sealed_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_skin_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'no_application_role',
        'photo.semantic_target_skin_sealed_registration_binding_state(uuid,uuid,text,jsonb)': 'no_application_role',
        'photo.lock_semantic_target_skin_sealed_evidence(uuid,uuid,jsonb)': 'no_application_role',
        'photo.seal_semantic_target_skin_preparation(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_semantic_target_skin_admission_evidence(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_v27_target_skin_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_skin_preparation_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'photo_scheduler',
        'photo.semantic_target_skin_registration_binding_state(uuid,uuid,text,jsonb)': 'photo_scheduler',
        'photo.semantic_target_face_saved(uuid,uuid,uuid,uuid,uuid,integer,text,text)': 'photo_api',
        'photo.semantic_target_face_registered_transport(uuid,uuid,uuid,uuid,uuid)': 'photo_scheduler',
        'photo.lock_semantic_target_skin_parser_completed(uuid,uuid,uuid,uuid,uuid,uuid,uuid)': 'photo_scheduler',
        'photo.semantic_target_face_completed_output_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.finish_semantic_target_face(uuid,uuid,jsonb,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_face_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text)': 'photo_api',
        'photo.lock_semantic_target_face_result(uuid,uuid,jsonb,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_face_result_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.lock_v27_target_face_sealed_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_face_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'no_application_role',
        'photo.semantic_target_face_sealed_registration_binding_state(uuid,uuid,text,jsonb)': 'no_application_role',
        'photo.lock_semantic_target_face_sealed_evidence(uuid,uuid,jsonb)': 'no_application_role',
        'photo.seal_semantic_target_face_preparation(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_semantic_target_face_admission_evidence(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_v27_target_face_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_face_preparation_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'photo_scheduler',
        'photo.semantic_target_face_registration_binding_state(uuid,uuid,text,jsonb)': 'photo_scheduler',
        'photo.semantic_target_detector_saved(uuid,uuid,uuid,uuid,uuid,integer,text)': 'photo_api',
        'photo.semantic_target_detector_registered_transport(uuid,uuid,uuid,uuid,uuid)': 'photo_scheduler',
        'photo.semantic_target_detector_completed_output_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.finish_semantic_target_detector(uuid,uuid,jsonb,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_detector_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text)': 'photo_api',
        'photo.lock_semantic_target_detector_result(uuid,uuid,jsonb,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_detector_result_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.semantic_target_detector_registration_binding_state(uuid,uuid,jsonb)': 'no_application_role',
        'photo.lock_semantic_target_detector_admission_evidence(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_v27_target_detector_sealed_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_detector_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'no_application_role',
        'photo.semantic_target_detector_sealed_registration_binding_state(uuid,uuid,jsonb)': 'no_application_role',
        'photo.lock_semantic_target_detector_sealed_evidence(uuid,uuid,jsonb)': 'no_application_role',
        'photo.seal_semantic_target_detector_preparation(uuid,uuid,jsonb)': 'photo_scheduler',

        'photo.lock_v27_target_detector_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_detector_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'photo_scheduler',
        'photo.semantic_target_person_saved(uuid,uuid,uuid,uuid,uuid,integer,text)': 'photo_api',
        'photo.semantic_target_person_registered_transport(uuid,uuid,uuid,uuid,uuid)': 'photo_scheduler',
        'photo.semantic_target_person_completed_output_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.finish_semantic_target_person(uuid,uuid,jsonb,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_person_canonical_outputs(uuid,uuid,uuid,uuid,uuid,text)': 'photo_api',
        'photo.lock_semantic_target_person_result(uuid,uuid,jsonb,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_person_result_ready(uuid,uuid,integer,bigint,jsonb)': 'no_application_role',
        'photo.lock_v27_target_person_sealed_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_person_sealed_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'no_application_role',
        'photo.semantic_target_person_sealed_registration_binding_state(uuid,uuid,jsonb)': 'no_application_role',
        'photo.lock_semantic_target_person_sealed_evidence(uuid,uuid,jsonb)': 'no_application_role',
        'photo.seal_semantic_target_person_preparation(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.semantic_target_person_registration_binding_state(uuid,uuid,jsonb)': 'no_application_role',
        'photo.lock_semantic_target_person_admission_evidence(uuid,uuid,jsonb)': 'photo_scheduler',
        'photo.lock_v27_target_person_graph_scope(uuid,uuid,uuid)': 'no_application_role',
        'photo.semantic_target_person_registration_state(uuid,uuid,uuid,uuid,text,text,jsonb,jsonb,jsonb,jsonb,jsonb,integer,text)': 'photo_scheduler',
        'photo.lock_v27_target_preparation_entry(uuid,uuid,jsonb,text,integer,text)': 'photo_scheduler',
        'photo.guard_v27_target_manifest()': 'no_application_role',
        'photo.v27_target_manifest_json_text(jsonb)': 'no_application_role',
        'photo.save_v27_target_manifest(uuid,uuid,jsonb,text)': 'photo_scheduler',
        'photo.v27_native_automatic_handoff_ready(uuid,uuid)': 'no_application_role',
        'photo.guard_semantic_native_protection_update()': 'no_application_role',
        'photo.semantic_native_original_person_result(uuid,uuid,uuid,uuid,uuid,text,uuid,uuid,text,uuid,text)': 'photo_api',
        'photo.lock_semantic_native_defect_person_authority(uuid,uuid,uuid,uuid,jsonb,jsonb,jsonb,jsonb,jsonb)': 'photo_scheduler',
        'photo.lock_semantic_native_person_input_objects(uuid,uuid,uuid,uuid,uuid,uuid,text,uuid,text,uuid,text,jsonb,jsonb)': 'photo_scheduler',
        'photo.v27_native_person_preparation_presence(uuid,uuid,uuid,uuid,uuid,text)': 'photo_api',
        'photo.enable_v27_person_admission(uuid,uuid,uuid,uuid,uuid,uuid)': 'photo_scheduler',
        'photo.claim_v27_person_admission(uuid,integer)': 'photo_scheduler',
        'photo.next_v27_person_admission_project(uuid)': 'photo_scheduler',
        'photo.record_v27_person_admission(uuid,uuid,uuid,text)': 'photo_scheduler',
        'photo.claim_v27_ordinary_parent(uuid,integer)': 'photo_scheduler',
        'photo.next_v27_ordinary_parent_project(uuid)': 'photo_scheduler',
        'photo.record_v27_ordinary_parent(uuid,uuid,uuid,text)': 'photo_scheduler',
    }
    assert set(after_functions)-set(before_functions) == set(expected_new)
    # Writers must acquire the global barrier before touching policy rows;
    # AFTER STATEMENT would invert selected policy SHARE locks against it.
    trigger = db.execute("""SELECT (tgtype::integer & 2)<>0, (tgtype::integer & 1)=0,
        tgfoid='photo.guard_v27_qualification_policy_write()'::regprocedure
        FROM pg_trigger WHERE tgrelid='photo.v2_qualification_policies'::regclass
        AND tgname='v27_qualification_policy_write' AND NOT tgisinternal""").fetchone()
    assert trigger == (True, True, True)
    for signature, allowed in expected_new.items():
        for role in ROLES:
            assert db.execute('SELECT has_function_privilege(%s,%s,\'EXECUTE\')',
                              (role, signature)).fetchone()[0] == (role == allowed)
    db.execute('SET LOCAL ROLE photo_scheduler')
    try:
        for function in ('claim_v27_person_admission', 'claim_v27_ordinary_parent'):
            assert db.execute(sql.SQL('SELECT * FROM photo.{}(%s,60)').format(sql.Identifier(function)),
                              (case['Project'],)).fetchall() == []
    finally:
        db.execute('RESET ROLE')
    assert tenant_inventory(db, tenant) == before_rows
    # Each rollback is intentionally refused without changing current schema/data.
    for migration in reversed(migrations):
        with pytest.raises(errors.RaiseException, match='Forward-only migration'):
            with db.transaction():
                db.execute(migration.read_text().split('-- +goose Down')[1])
    assert function_inventory(db) == after_functions
    assert tenant_inventory(db, tenant) == before_rows
