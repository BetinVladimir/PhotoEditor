"""Protocol/installation tests; native SDK, TLS sockets and keys are not mocked evidence."""
from hashlib import sha256
import ssl
import pytest
from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.provenance.installation import InstalledSigningOperator
from photo_worker.runtime.signer_challenge_http import SigningChallengeApplication, build_signer_challenge_server
from test_provenance_native_operator import setup_delegate


@pytest.mark.parametrize('mode', ['valid', 'client', 'policy', 'handle', 'algorithm', 'nonce', 'domain', 'extra', 'boolean_schema', 'noncanonical', 'native_revoked'])
def test_challenge_endpoint_requires_pinned_client_and_installed_policy(mode):
    live = True
    def sign(*args):
        nonlocal live
        if mode == 'native_revoked':live = False
        return b'proof'
    delegate, policy, _, _, _ = setup_delegate(inspect=lambda *args: live, sign=sign)
    installed = InstalledSigningOperator([canonical_bytes(policy)], lambda *args: True, delegate)
    peer = b'unit-only-peer-certificate'
    app = SigningChallengeApplication([canonical_bytes(policy)], installed, [sha256(peer).hexdigest()])
    payload = dict(domain='photoeditor.signing-operator-possession/v1',
        policy_sha256=sha256(canonical_bytes(policy)).hexdigest(), nonce='b'*64)
    if mode == 'policy':payload['policy_sha256'] = 'c'*64
    if mode == 'nonce':payload['nonce'] = 'short'
    if mode == 'domain':payload['domain'] = 'arbitrary-signature'
    request = dict(schema_version=1, key_handle=policy.key_handle, algorithm='ES256',
        payload_hex=canonical_bytes(payload).hex())
    if mode == 'client':peer = b'untrusted-client'
    if mode == 'handle':request['key_handle'] = 'other'
    if mode == 'algorithm':request['algorithm'] = 'ED25519'
    if mode == 'extra':request['extra'] = True
    if mode == 'boolean_schema':request['schema_version'] = True
    raw = canonical_bytes(request)
    if mode == 'noncanonical':raw += b' '
    if mode == 'valid':
        assert parse_json(app.handle(raw, peer)) == {'schema_version': 1, 'signature_hex': b'proof'.hex()}
    else:
        with pytest.raises((ValueError, PermissionError)):app.handle(raw, peer)


@pytest.mark.parametrize('mode', ['valid', 'client', 'policy', 'certificate', 'schema', 'noncanonical', 'revoked'])
def test_private_native_check_requires_live_installed_policy(mode):
    live = mode != 'revoked'
    delegate, policy, _, _, _ = setup_delegate(inspect=lambda *args: live)
    installed = InstalledSigningOperator([canonical_bytes(policy)], lambda *args: True, delegate)
    peer = b'unit-only-peer-certificate'
    app = SigningChallengeApplication([canonical_bytes(policy)], installed, [sha256(peer).hexdigest()])
    request = {'schema_version': 1, 'policy_sha256': sha256(canonical_bytes(policy)).hexdigest(),
               'certificate_chain_sha256': policy.certificate_chain_sha}
    if mode == 'client':peer = b'foreign-client'
    if mode == 'policy':request['policy_sha256'] = 'a' * 64
    if mode == 'certificate':request['certificate_chain_sha256'] = 'b' * 64
    if mode == 'schema':request['schema_version'] = True
    raw = canonical_bytes(request)
    if mode == 'noncanonical':raw += b' '
    if mode == 'valid':
        assert parse_json(app.check_native(raw, peer)) == request
    else:
        with pytest.raises((ValueError, PermissionError)):app.check_native(raw, peer)


def test_challenge_server_refuses_non_mtls_and_weak_tls_before_binding():
    delegate, policy, _, _, _ = setup_delegate()
    installed = InstalledSigningOperator([canonical_bytes(policy)], lambda *args: True, delegate)
    app = SigningChallengeApplication([canonical_bytes(policy)], installed, ['a'*64])
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    for minimum, verify in [(ssl.TLSVersion.TLSv1_3, ssl.CERT_NONE), (ssl.TLSVersion.TLSv1_2, ssl.CERT_REQUIRED)]:
        context.minimum_version, context.verify_mode = minimum, verify
        with pytest.raises(ValueError, match='TLS_INSTALLATION'):
            build_signer_challenge_server(('127.0.0.1', 0), app, context)


def test_challenge_application_owns_allowlist_and_refuses_duplicate_policies():
    delegate, policy, _, _, _ = setup_delegate()
    raw = canonical_bytes(policy)
    installed = InstalledSigningOperator([raw], lambda *args: True, delegate)
    pins = ['a'*64]
    app = SigningChallengeApplication([raw], installed, pins)
    pins.clear()
    assert app._clients == frozenset({'a'*64})
    with pytest.raises(ValueError, match='DUPLICATE'):
        SigningChallengeApplication([raw, raw], installed, ['a'*64])


def test_challenge_application_refuses_cancelled_scope_before_request_parsing():
    from photo_worker.operations.provenance.cancellation import signing_task_scope
    app=object.__new__(SigningChallengeApplication)
    live=True
    def active():
        if not live:raise RuntimeError('cancelled')
    with signing_task_scope(active):
        live=False
        with pytest.raises(RuntimeError,match='cancelled'):
            app.handle(b'{}',b'peer')
        live=True


def test_http_shutdown_after_signature_returns_503():
    from email.message import Message
    from io import BytesIO
    from threading import Event
    from time import monotonic
    from unittest.mock import patch
    application=object.__new__(SigningChallengeApplication)
    application.handle=lambda *args:b'{}'
    context=ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version=ssl.TLSVersion.TLSv1_3
    context.verify_mode=ssl.CERT_REQUIRED
    with patch('http.server.ThreadingHTTPServer.server_bind'), patch('http.server.ThreadingHTTPServer.server_activate'):
        server=build_signer_challenge_server(('127.0.0.1',0),application,context)
    class Stopping:
        calls=0
        def is_set(self):
            self.calls+=1
            return self.calls>=3
        def set(self):pass
    server.stopping=Stopping()
    handler=object.__new__(server.RequestHandlerClass)
    handler.server=server;handler.path='/internal/v1/operator-challenge'
    handler.rfile=BytesIO(b'{}');handler.expired=Event();handler.request_deadline=monotonic()+5
    handler.connection=type('Peer',(),{'getpeercert':lambda self,**kwargs:b'peer'})()
    headers=Message();headers['Content-Type']='application/json';headers['Content-Length']='2'
    handler.headers=headers
    status=[]
    handler.send_error=lambda code:status.append(code)
    handler.send_response=lambda code:pytest.fail('success after listener shutdown')
    try:handler.do_POST()
    finally:server.server_close()
    assert status==[503]


@pytest.mark.parametrize('field', ['verify_mode', 'minimum_version'])
def test_installed_tls_guard_refuses_live_policy_downgrade(field):
    import ssl
    from photo_worker.runtime.signer_challenge_http import build_signer_challenge_server
    # Reuse the protocol application fixture without opening a connection.
    from unittest.mock import patch
    from photo_worker.runtime.signer_challenge_http import SigningChallengeApplication
    application = object.__new__(SigningChallengeApplication)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.verify_mode = ssl.CERT_REQUIRED
    with patch('http.server.ThreadingHTTPServer.server_bind'), patch('http.server.ThreadingHTTPServer.server_activate'):
        server = build_signer_challenge_server(('127.0.0.1', 0), application, context)
    try:
        server.verify_tls()
        if field == 'verify_mode':context.verify_mode = ssl.CERT_NONE
        else:context.minimum_version = ssl.TLSVersion.TLSv1_2
        with pytest.raises(ValueError, match='TLS_INSTALLATION_CHANGED'):
            server.verify_tls()
    finally:
        server.server_close()


def test_tls_handshake_has_total_deadline_and_releases_slot(monkeypatch):
    from threading import Event, Thread
    from unittest.mock import patch
    closed, entered = Event(), Event()
    class Socket:
        def do_handshake(self):
            entered.set()
            assert closed.wait(3), 'total handshake deadline did not close transport'
            raise OSError('deadline closed transport')
        def shutdown(self, how):closed.set()
        def close(self):closed.set()
    secured, raw = Socket(), Socket()
    application = object.__new__(SigningChallengeApplication)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.verify_mode = ssl.CERT_REQUIRED
    monkeypatch.setattr(context, 'wrap_socket', lambda *args, **kwargs: secured)
    with patch('http.server.ThreadingHTTPServer.server_bind'), patch('http.server.ThreadingHTTPServer.server_activate'):
        server = build_signer_challenge_server(('127.0.0.1', 0), application, context,
                                              timeout=1, concurrency=1)
    assert server.slots.acquire(blocking=False)
    server.connections.add(raw)
    thread = Thread(target=server.process_request_thread, args=(raw, ('127.0.0.1', 1)))
    try:
        thread.start()
        assert entered.wait(1)
        assert closed.wait(3)
        thread.join(2)
        assert not thread.is_alive()
        assert not server.connections
        assert server.slots.acquire(blocking=False)
        server.slots.release()
    finally:
        closed.set()
        thread.join(2)
        server.server_close()
