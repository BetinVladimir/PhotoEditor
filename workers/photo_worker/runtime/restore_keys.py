"""Verify referenced encryption-key versions using installed privileged probes.

The probe must exercise actual restored key material (decrypt/known-answer), not
merely check a key ID exists. Never return key bytes or plaintext in evidence.
"""
from dataclasses import dataclass
import psycopg
from .restore_authority import require_restore_authority


@dataclass(frozen=True)
class RestoreKeyEvidence:
    schema_version: str
    database: str
    versions: tuple[tuple[str,str],...]


_SQL="""SELECT domain,key_id FROM (
 SELECT DISTINCT 'mail'::text AS domain,key_id FROM photo.mail_payloads
 WHERE octet_length(recipient_cipher)>0 OR octet_length(payload_cipher)>0
 UNION
 SELECT DISTINCT 'capture'::text AS domain,key_version AS key_id FROM photo.capture_challenges
 WHERE octet_length(nonce_ciphertext)>0
) keys ORDER BY domain COLLATE "C",key_id COLLATE "C"
"""


def verify_restored_key_versions(connection,*,expected_database,verify_key_version,
                                 assert_isolated_and_paused,check_active,
                                 maximum_versions=4096):
    if (not isinstance(expected_database,str) or not 1<=len(expected_database.encode())<=63
            or not callable(verify_key_version) or not callable(assert_isolated_and_paused)
            or not callable(check_active) or type(maximum_versions) is not int
            or not 1<=maximum_versions<=4096):raise ValueError('RESTORE_KEY_INSTALLATION')
    def guard():
        require_restore_authority(check_active,assert_isolated_and_paused)
    guard()
    if connection.info.transaction_status!=psycopg.pq.TransactionStatus.IDLE:
        raise ValueError('RESTORE_CONNECTION_NOT_IDLE')
    with connection.transaction():
        connection.execute('SET TRANSACTION ISOLATION LEVEL REPEATABLE READ READ ONLY')
        connection.execute('SET LOCAL row_security = off')
        connection.execute("SET LOCAL statement_timeout = '30s'")
        row=connection.execute('SELECT current_database(),(SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname=current_user)').fetchone()
        if row!=(expected_database,True):raise ValueError('RESTORE_KEY_DATABASE_AUTHORITY')
        versions=[];previous=None
        with connection.cursor(name='restore_key_versions') as cursor:
            cursor.itersize=128;cursor.execute(_SQL)
            for row in cursor:
                guard()
                if (len(row)!=2 or row[0] not in ('mail','capture') or not isinstance(row[1],str)
                        or not 1<=len(row[1].encode())<=256 or any(ord(c)<32 for c in row[1])):
                    raise ValueError('RESTORE_KEY_VERSION')
                version=(row[0],row[1]);order=tuple(v.encode() for v in version)
                if previous is not None and order<=previous:raise ValueError('RESTORE_KEY_ORDER')
                if len(versions)>=maximum_versions:raise ValueError('RESTORE_KEY_LIMIT')
                if verify_key_version(*version) is not True:raise ValueError('RESTORE_KEY_UNAVAILABLE')
                guard();versions.append(version);previous=order
        guard()
    guard()
    return RestoreKeyEvidence('restore-key-versions/v1',expected_database,tuple(versions))
