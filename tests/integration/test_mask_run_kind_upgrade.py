"""Forward upgrade restores established Mask.Create run kind only."""
from pathlib import Path
import re
import pytest
import psycopg
from psycopg import errors
from layer_original_fixture import seed_original_composition


@pytest.mark.migration_head(974)
def test_mask_kind_forward_upgrade_preserves_other_kinds_and_v2_gate(database):
    with psycopg.connect(**database) as db:
        case=seed_original_composition(db)
        def constraint(name):
            return db.execute("SELECT pg_get_constraintdef(oid) FROM pg_constraint WHERE conrelid='photo.runs'::regclass AND conname=%s",(name,)).fetchone()[0]
        old=constraint('runs_domain_0');guard=constraint('runs_v2_fail_fast')
        query="INSERT INTO photo.runs(tenant_id,kind,status,input_snapshot_id,config_snapshot,config_hash,created_by) VALUES(%s,%s,'queued',%s,'{}',%s,%s)"
        args=lambda kind:(case['Tenant'],kind,case['Snapshot'],'a'*64,case['Actor'])
        with pytest.raises(errors.CheckViolation),db.transaction():db.execute(query,args('mask'))
        migration=Path('backend/db/migrations/00975_restore_mask_run_kind.sql').read_text().split('-- +goose Down')[0]
        db.execute(migration)
        current=constraint('runs_domain_0')
        assert set(re.findall("'([^']+)'",current))==set(re.findall("'([^']+)'",old))|{'mask'}
        assert constraint('runs_v2_fail_fast')==guard
        db.execute(query,args('mask'))
        with pytest.raises(errors.CheckViolation),db.transaction():db.execute(query,args('unknown_kind'))
        with pytest.raises(errors.CheckViolation),db.transaction():db.execute(query,args('v2'))
