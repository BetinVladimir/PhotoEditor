"""Own removal qualification lifecycle; synthetic imported metadata, no GPU proof."""
import pytest
from psycopg.types.json import Jsonb
from removal_preview_activation_fixture import prepared_params_fixture
from test_v27_removal_preview_sql_roles import database, isolated_database_config


@pytest.mark.parametrize('mutation', ['policy_disabled','admission_disabled','license',
    'report_unverified','calibration_unverified','report_expired','calibration_expired'])
def test_removal_current_qualification_rechecked_after_revocation(database,mutation):
    args,run,scope=prepared_params_fixture(database)
    pin=scope['preview_profile']['qualification']
    def ready():
        with database.transaction():
            database.execute('SET LOCAL ROLE photo_fn_owner')
            result=database.execute('SELECT photo.v27_downstream_qualification_pin_ready(%s,%s,%s,%s,%s,%s)',
                (args[0],scope['project'],'semantic.preview_remove_object','1','cpu',Jsonb(pin))).fetchone()[0]
            database.execute('RESET ROLE')
            return result
    assert ready() is True
    if mutation=='policy_disabled':
        database.execute('UPDATE photo.v2_qualification_policies SET enabled=false,admission_enabled=false WHERE id=%s',(pin['policy_id'],))
    elif mutation=='admission_disabled':
        database.execute('UPDATE photo.v2_qualification_policies SET admission_enabled=false WHERE id=%s',(pin['policy_id'],))
    elif mutation=='license':
        database.execute('UPDATE photo.v2_qualification_policies SET license_cleared=false,admission_enabled=false WHERE id=%s',(pin['policy_id'],))
    else:
        ref=pin['report'] if mutation.startswith('report_') else pin['calibration']
        if mutation.endswith('unverified'):
            database.execute('UPDATE photo.artifacts SET verified_at=NULL WHERE tenant_id=%s AND id=%s',(args[0],ref['artifact_id']))
        else:
            database.execute("UPDATE photo.artifacts SET expires_at=clock_timestamp()-interval '1 second' WHERE tenant_id=%s AND id=%s",(args[0],ref['artifact_id']))
    if mutation in ('policy_disabled','license','admission_disabled'):
        enabled,licensed,admitted=database.execute('SELECT enabled,license_cleared,admission_enabled FROM photo.v2_qualification_policies WHERE id=%s',(pin['policy_id'],)).fetchone()
        assert admitted is False
        assert mutation!='policy_disabled' or enabled is False
        assert mutation!='license' or licensed is False
    before=database.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s',(run,)).fetchone()
    assert ready() is False
    assert database.execute('SELECT to_jsonb(r) FROM photo.runs r WHERE id=%s',(run,)).fetchone()==before
