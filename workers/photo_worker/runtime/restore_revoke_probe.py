"""Read-only learning-consent fixture baseline and committed-revoke observation."""
from dataclasses import dataclass
from uuid import UUID
import psycopg
from .restore_authority import require_restore_fixture


@dataclass(frozen=True)
class RestoreRevokeFixture:
    tenant_id: UUID
    project_id: UUID
    consent_id: UUID
    purpose: str
    person_id: UUID | None
    epoch: int
    revision: int
    policy_version: str
    actor_id: UUID
    learning_version_id: UUID


@dataclass(frozen=True)
class RestoreRevokeConsentEvidence:
    schema_version: str
    database: str
    tenant_id: UUID
    consent_id: UUID
    revoked: bool
    epoch: int
    revision: int
    affected_versions: int


def observe_fixture_learning_consent(connection,fixture,*,expected_database,revoked,
                                     assert_fixture_scope,check_active):
    if (not isinstance(fixture,RestoreRevokeFixture) or type(revoked) is not bool
            or any(not isinstance(v,UUID) or v.int==0 for v in (fixture.tenant_id,fixture.project_id,
                fixture.consent_id,fixture.actor_id,fixture.learning_version_id))
            or fixture.purpose not in ('learning_photographer','learning_person')
            or (fixture.purpose=='learning_photographer')!=(fixture.person_id is None)
            or (fixture.person_id is not None and (not isinstance(fixture.person_id,UUID) or fixture.person_id.int==0))
            or any(type(n) is not int or not 1<=n<9007199254740991 for n in (fixture.epoch,fixture.revision))
            or not isinstance(fixture.policy_version,str) or not 1<=len(fixture.policy_version)<=256
            or not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(assert_fixture_scope) or not callable(check_active)):
        raise ValueError('RESTORE_REVOKE_PROBE_INSTALLATION')
    def guard():require_restore_fixture(check_active,assert_fixture_scope,fixture)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    epoch=fixture.epoch+int(revoked);revision=fixture.revision+int(revoked)
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute("SET LOCAL statement_timeout='30s'")
        connection.execute("SET LOCAL lock_timeout='5s'")
        connection.execute('SET LOCAL row_security=off')
        row=connection.execute('SELECT current_database(),(SELECT rolsuper FROM pg_roles WHERE rolname=current_user)').fetchone()
        if row!=(expected_database,True):raise ValueError('RESTORE_REVOKE_PROBE_AUTHORITY')
        row=connection.execute('''SELECT g.purpose,g.person_id,g.status,g.epoch,g.revision,g.policy_version,g.actor_id
 FROM photo.consent_grants g JOIN photo.tenants t ON t.id=g.tenant_id AND t.status='active'
 WHERE g.tenant_id=%s AND g.project_id=%s AND g.id=%s''',
            (fixture.tenant_id,fixture.project_id,fixture.consent_id)).fetchone()
        if (not row or len(row)!=7 or row[:6]!=(fixture.purpose,fixture.person_id,'revoked' if revoked else 'granted',epoch,revision,fixture.policy_version)
                or (revoked and row[6]!=fixture.actor_id)):
            raise ValueError('RESTORE_REVOKE_PROBE_GRANT')
        guard()
        linked=connection.execute('''SELECT EXISTS(SELECT FROM photo.learning_example_links l
 JOIN photo.learning_versions v ON v.tenant_id=l.tenant_id AND v.id=l.version_id AND v.project_id=l.project_id
 WHERE l.tenant_id=%s AND l.project_id=%s AND l.consent_id=%s AND l.consent_epoch=%s AND l.version_id=%s)''',
            (fixture.tenant_id,fixture.project_id,fixture.consent_id,fixture.epoch,fixture.learning_version_id)).fetchone()
        if linked!=(True,):raise ValueError('RESTORE_REVOKE_PROBE_LINEAGE')
        if not revoked:
            row=connection.execute('''SELECT EXISTS(SELECT FROM photo.learning_versions v
 JOIN photo.learning_profiles p ON p.tenant_id=v.tenant_id AND p.id=v.profile_id AND p.active_version_id=v.id
 JOIN photo.version_eligibility e ON e.tenant_id=v.tenant_id AND e.version_id=v.id AND e.status='eligible'
 WHERE v.tenant_id=%s AND v.project_id=%s AND v.id=%s)''',
                (fixture.tenant_id,fixture.project_id,fixture.learning_version_id)).fetchone()
            if row!=(True,):raise ValueError('RESTORE_REVOKE_PROBE_BASELINE')
            affected=1
        else:
            row=connection.execute('''SELECT EXISTS(SELECT FROM photo.consent_events
 WHERE tenant_id=%s AND project_id=%s AND consent_id=%s AND epoch=%s
 AND action='revoke' AND actor_id=%s AND policy_version=%s)''',
                (fixture.tenant_id,fixture.project_id,fixture.consent_id,epoch,fixture.actor_id,fixture.policy_version)).fetchone()
            if row!=(True,):raise ValueError('RESTORE_REVOKE_PROBE_EVENT')
            row=connection.execute('''WITH affected AS (SELECT DISTINCT version_id FROM photo.learning_example_links
 WHERE tenant_id=%s AND project_id=%s AND consent_id=%s AND consent_epoch<=%s)
 SELECT count(*),count(*) FILTER (WHERE NOT EXISTS(SELECT FROM photo.version_eligibility e
 WHERE e.tenant_id=%s AND e.version_id=affected.version_id AND e.status='disabled')
 OR EXISTS(SELECT FROM photo.learning_profiles p WHERE p.tenant_id=%s AND p.active_version_id=affected.version_id))
 FROM affected''',(fixture.tenant_id,fixture.project_id,fixture.consent_id,fixture.epoch,fixture.tenant_id,fixture.tenant_id)).fetchone()
            if (not row or len(row)!=2 or type(row[0]) is not int or not 1<=row[0]<=10_000_000 or row[1]!=0):
                raise ValueError('RESTORE_REVOKE_PROBE_ACTIVE_DEPENDENCY')
            affected=row[0]
            row=connection.execute('''SELECT EXISTS(SELECT FROM photo.workflow_consent_links l
 JOIN photo.v2_workflows w ON w.tenant_id=l.tenant_id AND w.id=l.workflow_id
 JOIN photo.runs r ON r.tenant_id=w.tenant_id AND r.id=w.run_id
 WHERE l.tenant_id=%s AND l.project_id=%s AND l.consent_id=%s AND l.consent_epoch<=%s
 AND r.status IN ('draft','queued','running','waiting_review','cancelling') AND NOT r.cancel_requested)''',
                (fixture.tenant_id,fixture.project_id,fixture.consent_id,fixture.epoch)).fetchone()
            if row!=(False,):raise ValueError('RESTORE_REVOKE_PROBE_LIVE_WORKFLOW')
        guard()
    guard()
    return RestoreRevokeConsentEvidence('restore-revoke-consent/v1',expected_database,fixture.tenant_id,
        fixture.consent_id,revoked,epoch,revision,affected)
