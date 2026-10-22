"""Source-authored operator isolation scenarios; no live restore is run."""
from types import SimpleNamespace
import pytest
from photo_worker.runtime import backup_accept_operator as m


def config():
    return SimpleNamespace(database_url='postgresql://worker@restored-postgres:5432/isolated',
        amqp_url='amqps://user:secret@restored-rabbitmq:5671/vhost',
        gateway_origin='https://restored-gateway')


def test_transports_bind_worker_and_all_fixture_origins_to_actual_services(monkeypatch):
    seen = []
    monkeypatch.setattr(m, 'assert_probe_endpoint', lambda target, url, service, **kw:
                        seen.append((url, service)))
    m._assert_probe_transports(object(), config(), 'postgresql://operator@restored-postgres:5432/isolated',
        'isolated', {'legacy': {'origin': 'https://restore-api'},
                     'download': {'api_origin': 'https://restore-api', 'object_origin': 'https://restored-gateway'},
                     'purge': {'s3_target': {'origin': 'https://restored-minio:9000'}}})
    assert [service for _, service in seen] == ['postgres','postgres','rabbitmq','object-gateway',
                                               'caddy','caddy','object-gateway','minio']
    assert all('secret' not in url for url, _ in seen)


@pytest.mark.parametrize('dsn', ['postgresql://worker@restored-postgres:5432/production',
                                 'host=restored-postgres hostaddr=203.0.113.1 dbname=isolated'])
def test_database_alias_cannot_override_verified_container_address(monkeypatch, dsn):
    monkeypatch.setattr(m, 'assert_probe_endpoint', lambda *a, **kw: None)
    value = config(); value.database_url = dsn
    with pytest.raises(ValueError, match='RESTORE_ACCEPT_DATABASE_ENDPOINT'):
        m._assert_probe_transports(object(), value,
            'postgresql://operator@restored-postgres:5432/isolated', 'isolated', {})


def test_endpoint_denial_stops_before_any_probe(monkeypatch):
    def deny(*a, **kw):
        raise ValueError('RESTORE_PROBE_ENDPOINT_SCOPE')
    monkeypatch.setattr(m, 'assert_probe_endpoint', deny)
    with pytest.raises(ValueError, match='RESTORE_PROBE_ENDPOINT_SCOPE'):
        m._assert_probe_transports(object(), config(),
            'postgresql://operator@restored-postgres:5432/isolated', 'isolated', {})


def test_ambient_libpq_redirect_refused_before_config_or_worker(monkeypatch):
    monkeypatch.setenv('PGHOSTADDR', '203.0.113.1')
    with pytest.raises(ValueError, match='RESTORE_ACCEPT_AMBIENT_LIBPQ_FORBIDDEN'):
        m.run_operator_restore_acceptance('/missing/config', '0' * 64,
            settings={}, connection=object(), client=object(), target=object(),
            check_active=lambda: True)
