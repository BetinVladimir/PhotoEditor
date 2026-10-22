"""Staged operator acceptance installs real probes and embedded worker."""
from hashlib import sha256
import os
from threading import Event
import psycopg
from psycopg.conninfo import conninfo_to_dict
from urllib.parse import urlsplit, urlunsplit
from photo_worker.contracts.base import parse_json, canonical_bytes
from .restore_key_installation import _read_private, load_restore_key_probe
from .restore_probe_config import _record, _pin, load_restore_probe_dispatch
from .restore_recovery_load import load_pinned_restore_recovery
from .restore_native_config import install_restore_native_renderer
from .restore_embedded_worker import run_embedded_restore_acceptance
from .service import WorkerConfig, build_worker
from .backup_restore_operator import (start_isolated_probe_phase,
    stop_isolated_probe_phase, assert_isolated_probe_compose, assert_probe_endpoint)
from .deletion_history_store import DeletionHistoryStore
from .restore_deletion_catalogue import verify_restore_database_deletions
from .restore_consent_authority import SQLCurrentConsentAuthority
from .restore_keys import verify_restored_key_versions
from .backup_host import HostCommands, ComposeMaintenance


def _assert_probe_transports(probe, config, authority_dsn, expected_database, fixture):
    def endpoint(url, service, schemes, ports, default):
        parsed = urlsplit(url)
        host = parsed.hostname or ''
        if ':' in host:
            host = '[' + host + ']'
        # Supply protocol default explicitly to the host guard; no credentials
        # or signed query is needed for container/network identity checks.
        scoped = urlunsplit((parsed.scheme, host + ':' + str(parsed.port or default), '', '', ''))
        assert_probe_endpoint(probe, scoped, service, schemes=schemes, ports=ports)
    for dsn in (config.database_url, authority_dsn):
        parts = conninfo_to_dict(dsn)
        if parts.get('dbname') != expected_database or parts.get('hostaddr'):
            raise ValueError('RESTORE_ACCEPT_DATABASE_ENDPOINT')
        host = parts.get('host', '')
        if ':' in host:
            host = '[' + host + ']'
        endpoint('postgresql://' + host + ':' + parts.get('port','5432'),
                 'postgres', {'postgresql'}, {5432}, 5432)
    endpoint(config.amqp_url, 'rabbitmq', {'amqp','amqps'}, {5672,5671}, 5672)
    endpoint(config.gateway_origin, 'object-gateway', {'https'}, {443,8443}, 443)
    def walk(value, path=()):
        if isinstance(value, dict):
            for name, item in value.items():
                if name in ('origin','api_origin','object_origin'):
                    service = ('minio' if 's3_target' in path else
                               'object-gateway' if name == 'object_origin' else 'caddy')
                    ports = {443,9000} if service == 'minio' else {443,8443}
                    endpoint(item, service, {'https'}, ports, 443)
                else:
                    walk(item, path + (name,))
        elif isinstance(value, list):
            for item in value:
                walk(item, path)
    walk(fixture)


def _verify_operator_restore_authorities(settings,evidence,connection,consent_connection,*,assert_isolation,check_active):
    def active():
        if check_active() is False:raise RuntimeError('RESTORE_ACCEPT_OPERATOR_INACTIVE')
        return True
    def isolation():
        active()
        if assert_isolation() is False:raise ValueError('RESTORE_ACCEPT_NOT_ISOLATED')
        active()
    def independent_consent():
        isolation()
        row=consent_connection.execute('SELECT current_database(),system_identifier::text,pg_is_in_recovery() FROM pg_control_system()').fetchone()
        restored=connection.execute('SELECT current_database(),system_identifier::text FROM pg_control_system()').fetchone()
        if (row!=(settings['consent_database'],settings['consent_system_identifier'],False)
                or restored!=(settings['database'],settings['system_identifier']) or row[1]==restored[1]):
            raise ValueError('RESTORE_ACCEPT_CONSENT_AUTHORITY')
    consent=SQLCurrentConsentAuthority(consent_connection,expected_database=settings['consent_database'],
        assert_current_authority=independent_consent,check_active=active)
    history=DeletionHistoryStore(settings['independent_deletion_history_root'],check_active=active)
    isolation()
    with history.snapshot() as (head,entries):
        if head.sequence<evidence.deletion_checkpoint.sequence:
            raise ValueError('RESTORE_ACCEPT_DELETION_HEAD_REGRESSED')
        def retained_prefix():
            baseline=evidence.deletion_checkpoint
            if baseline.sequence==0 and baseline.head_sha256!='0'*64:
                raise ValueError('RESTORE_ACCEPT_DELETION_PREFIX')
            for entry in entries:
                if (entry['sequence']==baseline.sequence
                        and sha256(canonical_bytes(entry)).hexdigest()!=baseline.head_sha256):
                    raise ValueError('RESTORE_ACCEPT_DELETION_PREFIX')
                yield entry
        verify_restore_database_deletions(connection,retained_prefix(),head,
            expected_database=settings['database'],check_active=active)
    checkpoint=consent.latest_checkpoint()
    if (checkpoint.grants,checkpoint.sha256)!=(evidence.consent.grants,evidence.consent.sha256):
        raise ValueError('RESTORE_ACCEPT_CONSENT_HEAD_CHANGED')
    keys=load_restore_key_probe(settings['keys_path'],settings['witness_path'],
        settings['witness_sha256'],check_active=active)
    verify_restored_key_versions(connection,expected_database=settings['database'],
        verify_key_version=keys,assert_isolated_and_paused=isolation,check_active=active)
    isolation()
    return True


def verify_operator_restore_authorities(settings,evidence,connection,*,assert_isolation,check_active):
    """Current independent authority gate shared by acceptance and explicit cutover."""
    from .restore_recovery import RestoreComputeRecoveryEvidence
    if (not isinstance(evidence,RestoreComputeRecoveryEvidence)
            or not callable(assert_isolation) or not callable(check_active)):
        raise ValueError('RESTORE_ACCEPT_AUTHORITY_INSTALLATION')
    if (evidence.outbox.database!=settings['database']
            or evidence.preflight.storage.database.database!=settings['database']):
        raise ValueError('RESTORE_ACCEPT_AUTHORITY_SCOPE')
    if check_active() is False:raise RuntimeError('RESTORE_ACCEPT_OPERATOR_INACTIVE')
    if any(name.startswith('PG') for name in os.environ):
        raise ValueError('RESTORE_ACCEPT_AMBIENT_LIBPQ_FORBIDDEN')
    dsn=os.environ.get('PHOTO_RESTORE_CONSENT_DATABASE_URL')
    if not dsn:raise ValueError('RESTORE_ACCEPT_AUTHORITY_DSN')
    if assert_isolation() is False:raise ValueError('RESTORE_ACCEPT_NOT_ISOLATED')
    with psycopg.connect(dsn,autocommit=True,connect_timeout=5,
            options="-c statement_timeout=5000 -c lock_timeout=1000") as consent_connection:
        return _verify_operator_restore_authorities(settings,evidence,connection,consent_connection,
            assert_isolation=assert_isolation,check_active=check_active)


from .backup_operator_lock import restore_operator_locked


def _reject_ambient_libpq():
    if any(name.startswith('PG') for name in os.environ):
        raise ValueError('RESTORE_ACCEPT_AMBIENT_LIBPQ_FORBIDDEN')


@restore_operator_locked(preflight=_reject_ambient_libpq)
def run_operator_restore_acceptance(config_path, config_sha256, *, settings,
        connection, client, target, check_active, cleanup_connection=None):
    """Independent accept config is reviewed AFTER recovery/replay, never minted here.

    Existing operator connection is dedicated/autocommit; worker, qualification,
    and independent consent authority use separate connections. Writers remain
    isolated and are stopped again on every success/failure path.
    """
    stop = Event()
    def active():
        if stop.is_set() or check_active() is False:
            raise RuntimeError('RESTORE_ACCEPT_OPERATOR_INACTIVE')
        return True
    if any(name.startswith('PG') for name in os.environ):
        # libpq ambient service/hostaddr/options must not redirect an otherwise
        # container-bound explicit installation to another server or role.
        raise ValueError('RESTORE_ACCEPT_AMBIENT_LIBPQ_FORBIDDEN')
    raw = _read_private(config_path, 2 << 20, active)
    if sha256(raw).hexdigest() != _pin(config_sha256):
        raise ValueError('RESTORE_ACCEPT_OPERATOR_PIN')
    value = parse_json(raw, max_bytes=2 << 20)
    _record(value, ('schema_version', 'recovery', 'replay', 'probe', 'build_sha256',
                    'worker_env', 'native'))
    if value['schema_version'] != 'restore-accept-operator/v1' or value['build_sha256'] != settings['build_sha256']:
        raise ValueError('RESTORE_ACCEPT_OPERATOR_CONFIG')
    for name in ('recovery', 'replay', 'probe'):
        _record(value[name], ('path', 'sha256')); _pin(value[name]['sha256'])
    evidence, receipt = load_pinned_restore_recovery(value['recovery']['path'],
        value['recovery']['sha256'], value['replay']['path'], value['replay']['sha256'],
        build_sha256=value['build_sha256'], check_active=active)
    if receipt.database != settings['database']:
        raise ValueError('RESTORE_ACCEPT_OPERATOR_DATABASE')
    if not isinstance(value['worker_env'], dict) or any(not isinstance(k,str) or not isinstance(v,str)
                                                       for k,v in value['worker_env'].items()):
        raise ValueError('RESTORE_ACCEPT_WORKER_ENV')
    config = WorkerConfig.from_env(value['worker_env'])
    probe_raw = _read_private(value['probe']['path'], 1 << 20, active)
    if sha256(probe_raw).hexdigest() != value['probe']['sha256']:
        raise ValueError('RESTORE_ACCEPT_PROBE_PIN')
    probe_value = parse_json(probe_raw, max_bytes=1 << 20)
    if not isinstance(probe_value, dict) or probe_value.get('buckets') != settings['source_buckets']:
        raise ValueError('RESTORE_ACCEPT_BUCKET_SCOPE')
    authority_dsn = os.environ.get('PHOTO_RESTORE_DATABASE_URL')
    consent_dsn = os.environ.get('PHOTO_RESTORE_CONSENT_DATABASE_URL')
    if not authority_dsn or not consent_dsn:
        raise ValueError('RESTORE_ACCEPT_AUTHORITY_DSN')
    # Construct the storage adapter from the pinned installation, rather than
    # accepting an opaque caller client that could address another namespace.
    from minio import Minio
    import urllib3
    access = os.environ.get('PHOTO_RESTORE_SOURCE_ACCESS_KEY')
    secret = os.environ.get('PHOTO_RESTORE_SOURCE_SECRET_KEY')
    endpoint = settings['source_endpoint']
    if not access or not secret or '://' in endpoint or '/' in endpoint:
        raise ValueError('RESTORE_ACCEPT_STORAGE_INSTALLATION')
    # Fixture reads/mutations must not inherit backup transfer retries/timeouts.
    client = Minio(endpoint, access_key=access, secret_key=secret, secure=True,
        http_client=urllib3.PoolManager(timeout=urllib3.Timeout(connect=5, read=5),
            retries=False, cert_reqs='CERT_REQUIRED'))
    # The phase guards share cancellation and use bounded host commands.
    commands = HostCommands(target.commands.tools, check_active=active, timeout=10)
    target = ComposeMaintenance(commands, target.file, target.pin,
        target.project, target.rendered_pin)
    connection.execute("SET statement_timeout='5s'")
    connection.execute("SET lock_timeout='1s'")
    if cleanup_connection is None or cleanup_connection is connection:
        raise ValueError('RESTORE_ACCEPT_CLEANUP_CONNECTION_REQUIRED')
    cleanup_connection.execute("SET statement_timeout='5s'")
    cleanup_connection.execute("SET lock_timeout='1s'")
    cleanup_identity = cleanup_connection.execute("SELECT current_database(),system_identifier::text,(SELECT rolsuper FROM pg_roles WHERE rolname=current_user) FROM pg_control_system()").fetchone()
    if (not cleanup_identity or cleanup_identity[0] == settings['database']
            or cleanup_identity[1:] != (settings['system_identifier'], True)):
        raise ValueError('RESTORE_ACCEPT_CLEANUP_AUTHORITY')
    probe = None
    try:
        probe = start_isolated_probe_phase(settings, target, connection)
        def isolation():
            active(); assert_isolated_probe_compose(probe)
            if sha256(_read_private(config_path, 2 << 20, active)).hexdigest() != config_sha256:
                raise ValueError('RESTORE_ACCEPT_OPERATOR_CHANGED')
            _assert_probe_transports(probe, config, authority_dsn, settings['database'], probe_value)
            source = urlsplit('https://' + settings['source_endpoint'])
            source_host = source.hostname or ''
            if ':' in source_host:
                source_host = '[' + source_host + ']'
            endpoint = urlunsplit(('https', source_host + ':' + str(source.port or 443), '', '', ''))
            assert_probe_endpoint(probe, endpoint, 'minio', schemes={'https'}, ports={443,9000})
            active()
        with psycopg.connect(consent_dsn, autocommit=True, connect_timeout=5,
                options="-c statement_timeout=5000 -c lock_timeout=1000") as consent_connection:
            def current_authorities():
                return _verify_operator_restore_authorities(settings,evidence,connection,consent_connection,
                    assert_isolation=isolation,check_active=active)
            current_authorities()
            installation, barrier = install_restore_native_renderer(value['native'], config,
                probe_value=probe_value, authority_database_url=authority_dsn,
                expected_database=settings['database'], assert_isolation=isolation, check_active=active)
            dispatch, pins = load_restore_probe_dispatch(value['probe']['path'], value['probe']['sha256'],
                connection=connection, client=client, replay=receipt, renderer_barrier=barrier,
                assert_probe_isolation=isolation, check_active=active)
            database, transport = build_worker(config, render_renderer=barrier)
            def cancel_probe_io():
                failures = []
                for handle in (connection, consent_connection):
                    try:
                        handle.cancel_safe(timeout=5)
                    except BaseException as exc:
                        failures.append(exc)
                    finally:
                        try:
                            handle.close()
                        except BaseException as exc:
                            failures.append(exc)
                if failures:
                    raise RuntimeError('RESTORE_ACCEPT_SQL_CANCEL_FAILED') from failures[0]
            return run_embedded_restore_acceptance(config, database, transport,
                dispatch=dispatch, renderer_barrier=barrier,
                recovery_path=value['recovery']['path'], recovery_sha256=value['recovery']['sha256'],
                replay_path=value['replay']['path'], replay_sha256=value['replay']['sha256'],
                evidence=evidence, build_sha256=value['build_sha256'], fixture_pins=pins,
                assert_probe_isolation=isolation, verify_current_authorities=current_authorities,
                check_active=check_active, stop_event=stop, cancel_probe_io=cancel_probe_io)
    finally:
        if probe is not None:
            stop_isolated_probe_phase(settings, probe, cleanup_connection)
