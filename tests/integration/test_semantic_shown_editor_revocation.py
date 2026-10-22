"""Current editor authority on real claimed shown tasks, metadata-only outputs."""
from uuid import uuid4

import psycopg
import pytest

from conftest import database
from test_database import tenant as create_tenant
from test_v27_retouch_adoption_commit import successful_copy_metadata_fixture


@pytest.mark.parametrize('withdrawal', ['suspend', 'viewer', 'remove'])
def test_shown_evidence_rechecks_non_owner_editor_membership(database, monkeypatch, withdrawal):
    # Inject a different bootstrap actor before any workflow/receipt is built.
    # The real tenant owner stays active; no owner invariant or SQL authority
    # predicate is replaced to manufacture a revocation scenario.
    import test_v27_semantic_final_producer_gate as producer_fixture
    import semantic_qualification_fixture as qualification_fixture

    owners = {}

    def editor_tenant(db, email):
        tenant, owner = create_tenant(db, email)
        editor = db.execute(
            'INSERT INTO photo.users(email_normalized) VALUES(%s) RETURNING id',
            ('shown-editor-' + str(uuid4()) + '@example.test',),
        ).fetchone()[0]
        db.execute(
            "INSERT INTO photo.memberships(tenant_id,user_id,role) VALUES(%s,%s,'editor')",
            (tenant, editor),
        )
        owners[tenant] = owner
        return tenant, editor

    monkeypatch.setattr(producer_fixture, 'tenant', editor_tenant)
    import_qualification = qualification_fixture.frozen_qualification

    def owner_qualification(db, tenant, project, actor, *args, **kwargs):
        # Operator qualification remains the real owner's separate action;
        # editing a project never grants an editor operator-import authority.
        return import_qualification(db, tenant, project, owners.get(tenant, actor), *args, **kwargs)

    monkeypatch.setattr(qualification_fixture, 'frozen_qualification', owner_qualification)
    # These fixture modules retain an imported alias across parameterized cases
    # and earlier quota tests, so explicitly bind their operator action too.
    import retouch_preview_admission_fixture
    import retouch_physical_apply_fixture
    monkeypatch.setattr(retouch_preview_admission_fixture, 'frozen_qualification', owner_qualification)
    monkeypatch.setattr(retouch_physical_apply_fixture, 'frozen_qualification', owner_qualification)
    with psycopg.connect(**database) as db:
        values, _, commit = successful_copy_metadata_fixture(db)
        tenant, task, attempt, fence = commit[:4]
        actor, session = values[0][0][2:4]
        owner = owners[tenant]
        assert actor != owner
        assert db.execute(
            'SELECT owner_user_id FROM photo.tenants WHERE id=%s', (tenant,),
        ).fetchone() == (owner,)

        def evidence():
            with db.transaction():
                db.execute('SET LOCAL ROLE photo_scheduler')
                inputs = db.execute(
                    'SELECT * FROM photo.v27_shown_adoption_artifact(%s,%s,%s,%s,%s,%s)',
                    (tenant, task, attempt, fence, commit[7], 'semantic.adopt_shown_retouch_region'),
                ).fetchall()
                outputs = db.execute(
                    'SELECT * FROM photo.v27_shown_adoption_staged_outputs(%s,%s,%s,%s,%s)',
                    (tenant, task, attempt, fence, 'semantic.adopt_shown_retouch_region'),
                ).fetchall()
                db.execute('RESET ROLE')
                return inputs, outputs

        before = evidence()
        assert len(before[0]) == 1 and before[0][0][-1] == 'input'
        assert len(before[1]) == 2 and all(row[2] == 'ready' for row in before[1])
        statements = {
            'suspend': "UPDATE photo.memberships SET status='suspended' WHERE tenant_id=%s AND user_id=%s",
            'viewer': "UPDATE photo.memberships SET role='viewer' WHERE tenant_id=%s AND user_id=%s",
            'remove': 'DELETE FROM photo.memberships WHERE tenant_id=%s AND user_id=%s',
        }
        with db.transaction(force_rollback=True):
            assert db.execute(statements[withdrawal], (tenant, actor)).rowcount == 1
            assert db.execute(
                'SELECT revoked_at IS NULL AND expires_at>clock_timestamp() FROM photo.sessions WHERE id=%s',
                (session,),
            ).fetchone() == (True,)
            assert db.execute(
                "SELECT role,status FROM photo.memberships WHERE tenant_id=%s AND user_id=%s",
                (tenant, owner),
            ).fetchone() == ('owner', 'active')
            assert evidence() == ([], [])
        assert evidence() == before
