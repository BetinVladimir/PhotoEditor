"""Frozen naming expansion; authored only, no migration execution credited."""
import pytest

@pytest.mark.parametrize('template,expected', [('{original_name}_edited','portrait_edited'),('print_{ordinal}_{sha8}','print_0007_aaaaaaaa'),('literal','literal')])
def test_frozen_export_name_expands_known_tokens(db,template,expected):
    assert db.execute('SELECT photo.download_export_basename(%s,%s,%s,%s)',('portrait.RAW','a'*64,7,template)).fetchone()[0]==expected


def test_grouped_export_journal_is_append_only_and_tenant_scoped(db):
    for table in ('export_delivery_batches','export_delivery_tickets'):
        assert db.execute('SELECT relrowsecurity AND relforcerowsecurity FROM pg_class WHERE oid=%s::regclass',('photo.'+table,)).fetchone()[0]
        assert db.execute('SELECT has_table_privilege(%s,%s,%s)',('photo_api','photo.'+table,'SELECT,INSERT')).fetchone()[0]
        assert not db.execute('SELECT has_table_privilege(%s,%s,%s)',('photo_api','photo.'+table,'UPDATE')).fetchone()[0]
        assert not db.execute('SELECT has_table_privilege(%s,%s,%s)',('photo_api','photo.'+table,'DELETE')).fetchone()[0]
    for role in ('photo_api','photo_portal','photo_worker','photo_scheduler'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,'photo.delivery_export_basename(uuid,uuid,integer,uuid,text)','EXECUTE')).fetchone()[0]
    purge=db.execute("SELECT pg_get_functiondef('photo.finish_tenant_deletion(uuid,uuid)'::regprocedure)").fetchone()[0]
    assert purge.index('DELETE FROM photo.export_delivery_tickets') < purge.index('DELETE FROM photo.export_delivery_batches')


def test_delivery_physical_failure_gate_has_no_public_or_worker_authority(db):
    signature='photo.fail_v30_delivery_physical_source(uuid,text,uuid,uuid,uuid,text,text,text,integer,text,boolean)'
    for role in ('photo_api','photo_portal','photo_worker','photo_gateway','photo_maintenance'):
        assert not db.execute('SELECT has_function_privilege(%s,%s,%s)',(role,signature,'EXECUTE')).fetchone()[0]
    assert db.execute('SELECT has_function_privilege(%s,%s,%s)',('photo_scheduler',signature,'EXECUTE')).fetchone()[0]
    # An arbitrary exact-looking object cannot fail any package without a live
    # expansion token, runtime lease, adopted source inventory and source scope.
    assert not db.execute("SELECT photo.fail_v30_delivery_physical_source(gen_random_uuid(),'photo',gen_random_uuid(),gen_random_uuid(),gen_random_uuid(),%s,'version','SOURCE_OBJECT_MISSING',0,'{}',false)",('a'*64,)).fetchone()[0]
