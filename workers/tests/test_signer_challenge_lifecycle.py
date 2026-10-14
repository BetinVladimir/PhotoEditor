"""Listener ownership tests; transport/native acceptance tested separately."""
from threading import Event
import pytest
from photo_worker.runtime import signer_challenge_lifecycle as lifecycle


@pytest.mark.parametrize('mode', ['normal', 'worker_failure', 'listener_failure', 'cancelled', 'tls_changed'])
def test_listener_scope_owns_shutdown_and_invalidates_guard(monkeypatch, mode):
    stop, failed = Event(), Event()
    calls = []
    changed = []
    class Server:
        stopping = Event()
        def verify_tls(self):
            if changed:raise ValueError('SIGNER_CHALLENGE_TLS_INSTALLATION_CHANGED')
        def serve_forever(self, **kwargs):
            calls.append('serve')
            if mode == 'listener_failure':
                failed.set()
                raise RuntimeError('private transport exception')
            stop.wait(3)
        def shutdown(self):
            calls.append('shutdown');self.stopping.set();stop.set()
        def server_close(self):
            calls.append('close');self.stopping.set();stop.set()
    server = Server()
    monkeypatch.setattr(lifecycle, 'build_signer_challenge_server', lambda *args, **kwargs: server)
    cancelled = False
    def check():
        if cancelled:raise ValueError('cancelled')
    guard = None
    try:
        with lifecycle.running_signer_challenges(None, None, None, check_active=check) as active:
            guard = active
            if mode == 'listener_failure':
                assert failed.wait(1)
                active()
            if mode == 'tls_changed':
                changed.append(True)
                active()
            if mode == 'cancelled':
                cancelled = True
                active()
            if mode == 'worker_failure':raise ValueError('worker failure')
            active()
    except (ValueError, RuntimeError):
        assert mode != 'normal'
    else:
        assert mode == 'normal'
    assert 'close' in calls and server.stopping.is_set()
    if guard is not None:
        with pytest.raises((ValueError, RuntimeError)):guard()


def test_cancelled_before_bind_does_not_create_listener(monkeypatch):
    def unexpected(*args, **kwargs):raise AssertionError('must not bind')
    monkeypatch.setattr(lifecycle, 'build_signer_challenge_server', unexpected)
    def cancelled():raise ValueError('cancelled')
    with pytest.raises(ValueError, match='cancelled'):
        with lifecycle.running_signer_challenges(None, None, None, check_active=cancelled):
            pytest.fail('must not enter')


def test_explicit_false_before_bind_does_not_create_listener(monkeypatch):
    monkeypatch.setattr(lifecycle, 'build_signer_challenge_server',
        lambda *args, **kwargs: pytest.fail('listener bound after refusal'))
    with pytest.raises(RuntimeError, match='SIGNER_TASK_INACTIVE'):
        with lifecycle.running_signer_challenges(None, None, None, check_active=lambda: False):
            pytest.fail('refused scope entered')


def test_explicit_false_after_bind_invalidates_listener_guard(monkeypatch):
    stop = Event()
    class Server:
        stopping = Event()
        def verify_tls(self):pass
        def serve_forever(self, **kwargs):stop.wait(3)
        def shutdown(self):self.stopping.set();stop.set()
        def server_close(self):self.stopping.set();stop.set()
    server = Server()
    monkeypatch.setattr(lifecycle, 'build_signer_challenge_server',
        lambda *args, **kwargs: server)
    allowed = True
    def check():return allowed
    with lifecycle.running_signer_challenges(None, None, None, check_active=check) as active:
        allowed = False
        with pytest.raises(RuntimeError, match='SIGNER_TASK_INACTIVE'):
            active()
        allowed = True
    assert server.stopping.is_set()


def test_retained_listener_guard_refuses_before_tls_access_after_close(monkeypatch):
    stop=Event();checks=[]
    class Server:
        stopping=Event()
        def verify_tls(self):
            if self.stopping.is_set():raise AssertionError('TLS read after close')
            checks.append('tls')
        def serve_forever(self,**kwargs):stop.wait(3)
        def shutdown(self):self.stopping.set();stop.set()
        def server_close(self):self.stopping.set();stop.set()
    server=Server()
    monkeypatch.setattr(lifecycle,'build_signer_challenge_server',lambda *args,**kwargs:server)
    with lifecycle.running_signer_challenges(None,None,None) as active:
        active()
    before=len(checks)
    with pytest.raises(RuntimeError,match='SIGNER_CHALLENGE_LISTENER_STOPPED'):
        active()
    assert len(checks)==before


@pytest.mark.parametrize('field,value', [
    ('address', ('localhost', True)), ('address', ('localhost', 65536)),
    ('address', ('bad host', 443)), ('timeout', float('nan')), ('timeout', True),
    ('concurrency', 0), ('client_certificate_shas', ['a'*64, 'a'*64]),
    ('client_certificate_shas', ['A'*64]), ('tls_context', None)])
def test_listener_installation_refuses_invalid_configuration_before_resources(field, value):
    import ssl
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.verify_mode = ssl.CERT_REQUIRED
    args = dict(address=('127.0.0.1', 0), tls_context=context,
                client_certificate_shas=['a'*64], timeout=10, concurrency=4)
    args[field] = value
    with pytest.raises(ValueError, match='LISTENER_INSTALLATION'):
        lifecycle.SigningChallengeListenerInstallation(**args)
