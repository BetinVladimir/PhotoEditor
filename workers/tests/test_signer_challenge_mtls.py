"""Real loopback TLS transport tests; synthetic CA and native signing double only."""
from datetime import datetime, timedelta, timezone
from hashlib import sha256
from http.client import HTTPSConnection
import ssl
import socket
from threading import Thread, Event

import pytest
from cryptography import x509
from cryptography.hazmat.primitives import hashes, serialization
from cryptography.hazmat.primitives.asymmetric import ec
from cryptography.x509.oid import ExtendedKeyUsageOID, NameOID

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.operations.provenance.installation import InstalledSigningOperator
from photo_worker.runtime.signer_challenge_http import SigningChallengeApplication, build_signer_challenge_server
from test_provenance_native_operator import setup_delegate


def tls_material(tmp_path):
    now = datetime.now(timezone.utc)
    ca_key = ec.generate_private_key(ec.SECP256R1())
    name = x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, 'challenge test CA')])
    ca = (x509.CertificateBuilder().subject_name(name).issuer_name(name)
          .public_key(ca_key.public_key()).serial_number(x509.random_serial_number())
          .not_valid_before(now-timedelta(minutes=1)).not_valid_after(now+timedelta(hours=1))
          .add_extension(x509.BasicConstraints(ca=True, path_length=0), critical=True)
          .sign(ca_key, hashes.SHA256()))
    ca_path = tmp_path / 'ca.pem'
    ca_path.write_bytes(ca.public_bytes(serialization.Encoding.PEM))
    def issue(label, usage):
        key = ec.generate_private_key(ec.SECP256R1())
        cert = (x509.CertificateBuilder()
                .subject_name(x509.Name([x509.NameAttribute(NameOID.COMMON_NAME, label)]))
                .issuer_name(name).public_key(key.public_key()).serial_number(x509.random_serial_number())
                .not_valid_before(now-timedelta(minutes=1)).not_valid_after(now+timedelta(hours=1))
                .add_extension(x509.BasicConstraints(ca=False, path_length=None), critical=True)
                .add_extension(x509.ExtendedKeyUsage([usage]), critical=False)
                .add_extension(x509.SubjectAlternativeName([x509.DNSName('localhost')]), critical=False)
                .sign(ca_key, hashes.SHA256()))
        cert_path, key_path = tmp_path / (label+'.pem'), tmp_path / (label+'.key')
        cert_path.write_bytes(cert.public_bytes(serialization.Encoding.PEM))
        key_path.write_bytes(key.private_bytes(serialization.Encoding.PEM,
                            serialization.PrivateFormat.PKCS8, serialization.NoEncryption()))
        key_path.chmod(0o600)
        return cert_path, key_path, sha256(cert.public_bytes(serialization.Encoding.DER)).hexdigest()
    server = issue('server', ExtendedKeyUsageOID.SERVER_AUTH)
    client = issue('client', ExtendedKeyUsageOID.CLIENT_AUTH)
    other = issue('other', ExtendedKeyUsageOID.CLIENT_AUTH)
    context = ssl.SSLContext(ssl.PROTOCOL_TLS_SERVER)
    context.minimum_version = ssl.TLSVersion.TLSv1_3
    context.verify_mode = ssl.CERT_REQUIRED
    context.load_verify_locations(cafile=str(ca_path))
    context.load_cert_chain(str(server[0]), str(server[1]))
    def client_context(identity):
        result = ssl.create_default_context(cafile=str(ca_path))
        result.minimum_version = ssl.TLSVersion.TLSv1_3
        if identity is not None:result.load_cert_chain(str(identity[0]), str(identity[1]))
        return result
    return context, client_context, client, other


@pytest.mark.parametrize('mode', ['valid', 'missing_identity', 'unlisted_identity', 'wrong_path',
                                  'duplicate_length', 'encoding', 'headers_limit', 'body_limit'])
def test_real_socket_requires_mtls_pin_and_closed_http_envelope(tmp_path, mode):
    server_tls, client_context, identity, other = tls_material(tmp_path)
    calls = []
    delegate, policy, _, _, _ = setup_delegate(sign=lambda *args: calls.append(args) or b'proof')
    policy_raw = canonical_bytes(policy)
    operator = InstalledSigningOperator([policy_raw], lambda *args: True, delegate)
    app = SigningChallengeApplication([policy_raw], operator, [identity[2]])
    server = build_signer_challenge_server(('127.0.0.1', 0), app, server_tls, timeout=1)
    thread = Thread(target=server.serve_forever, kwargs={'poll_interval': 0.01}, daemon=True)
    thread.start()
    connection = HTTPSConnection('localhost', server.server_port, timeout=3,
        context=client_context(None if mode == 'missing_identity' else other if mode == 'unlisted_identity' else identity))
    payload = canonical_bytes(dict(domain='photoeditor.signing-operator-possession/v1',
        policy_sha256=sha256(policy_raw).hexdigest(), nonce='b'*64))
    body = canonical_bytes(dict(schema_version=1, key_handle=policy.key_handle,
                                algorithm='ES256', payload_hex=payload.hex()))
    try:
        if mode == 'missing_identity':
            with pytest.raises((ssl.SSLError, OSError, EOFError)):
                connection.request('POST', '/internal/v1/operator-challenge', body,
                                   {'Content-Type': 'application/json'})
                connection.getresponse().read()
        else:
            connection.putrequest('POST', '/wrong' if mode == 'wrong_path' else '/internal/v1/operator-challenge')
            connection.putheader('Content-Type', 'application/json')
            connection.putheader('Content-Length', '9217' if mode == 'body_limit' else str(len(body)))
            if mode == 'duplicate_length':connection.putheader('Content-Length', str(len(body)))
            if mode == 'encoding':connection.putheader('Content-Encoding', 'gzip')
            if mode == 'headers_limit':connection.putheader('X-Padding', 'x'*8192)
            connection.endheaders(body)
            response = connection.getresponse()
            raw = response.read()
            expected = 200 if mode == 'valid' else 403 if mode == 'unlisted_identity' else 431 if mode == 'headers_limit' else 400
            assert response.status == expected
            if mode == 'valid':
                assert parse_json(raw) == {'schema_version': 1, 'signature_hex': b'proof'.hex()}
                assert response.getheader('Cache-Control') == 'no-store'
                assert response.getheader('Connection') == 'close'
        assert len(calls) == (1 if mode == 'valid' else 0)
    finally:
        connection.close()
        server.shutdown()
        server.server_close()
        thread.join(timeout=3)
        assert not thread.is_alive()


def test_stalled_client_hello_does_not_block_other_admitted_clients(tmp_path):
    server_tls, client_context, identity, _ = tls_material(tmp_path)
    delegate, policy, _, _, signatures = setup_delegate()
    raw = canonical_bytes(policy)
    app = SigningChallengeApplication([raw],
        InstalledSigningOperator([raw], lambda *args: True, delegate), [identity[2]])
    server = build_signer_challenge_server(('127.0.0.1', 0), app, server_tls,
                                           timeout=3, concurrency=2)
    thread = Thread(target=server.serve_forever, kwargs={'poll_interval': 0.01}, daemon=True)
    thread.start()
    stalled = socket.create_connection(('127.0.0.1', server.server_port), timeout=1)
    # Keep one raw connection open without sending ClientHello. The second slot
    # must complete before that peer's three-second handshake timeout expires.
    connection = HTTPSConnection('localhost', server.server_port, timeout=1,
                                  context=client_context(identity))
    payload = canonical_bytes(dict(domain='photoeditor.signing-operator-possession/v1',
        policy_sha256=sha256(raw).hexdigest(), nonce='b'*64))
    body = canonical_bytes(dict(schema_version=1, key_handle=policy.key_handle,
                                algorithm='ES256', payload_hex=payload.hex()))
    try:
        connection.request('POST', '/internal/v1/operator-challenge', body,
                           {'Content-Type': 'application/json'})
        response = connection.getresponse()
        assert response.status == 200
        assert parse_json(response.read())['signature_hex'] == b'signature'.hex()
        assert len(signatures) == 1
    finally:
        connection.close()
        stalled.close()
        server.shutdown()
        server.server_close()
        thread.join(timeout=3)
        assert not thread.is_alive()


def test_stop_during_external_key_call_suppresses_challenge_response(tmp_path):
    from http.client import HTTPException
    server_tls, client_context, identity, _ = tls_material(tmp_path)
    calls = []
    server = None
    def sign(*args):
        calls.append(args)
        server.stop_connections()
        return b'proof-after-stop'
    delegate, policy, _, _, _ = setup_delegate(sign=sign)
    raw = canonical_bytes(policy)
    app = SigningChallengeApplication([raw],
        InstalledSigningOperator([raw], lambda *args: True, delegate), [identity[2]])
    server = build_signer_challenge_server(('127.0.0.1', 0), app, server_tls, timeout=1)
    thread = Thread(target=server.serve_forever, kwargs={'poll_interval': 0.01}, daemon=True)
    thread.start()
    connection = HTTPSConnection('localhost', server.server_port, timeout=3,
                                  context=client_context(identity))
    payload = canonical_bytes(dict(domain='photoeditor.signing-operator-possession/v1',
        policy_sha256=sha256(raw).hexdigest(), nonce='b'*64))
    body = canonical_bytes(dict(schema_version=1, key_handle=policy.key_handle,
                                algorithm='ES256', payload_hex=payload.hex()))
    try:
        with pytest.raises((HTTPException, OSError)):
            connection.request('POST', '/internal/v1/operator-challenge', body,
                               {'Content-Type': 'application/json'})
            connection.getresponse().read()
        assert server.stopping.is_set()
        assert len(calls) == 1
    finally:
        connection.close()
        server.shutdown()
        server.server_close()
        server.server_close()  # Closure is idempotent.
        thread.join(timeout=3)
        assert not thread.is_alive()


@pytest.mark.parametrize('phase', ['headers', 'body'])
def test_http_deadline_closes_incomplete_request_without_key_call(tmp_path, phase):
    from time import monotonic
    server_tls, client_context, identity, _ = tls_material(tmp_path)
    delegate, policy, _, _, signatures = setup_delegate()
    raw = canonical_bytes(policy)
    app = SigningChallengeApplication([raw],
        InstalledSigningOperator([raw], lambda *args: True, delegate), [identity[2]])
    server = build_signer_challenge_server(('127.0.0.1', 0), app, server_tls, timeout=1)
    thread = Thread(target=server.serve_forever, kwargs={'poll_interval': 0.01}, daemon=True)
    thread.start()
    connection = client_context(identity).wrap_socket(
        socket.create_connection(('127.0.0.1', server.server_port), timeout=3),
        server_hostname='localhost')
    try:
        start = monotonic()
        prefix = b'POST /internal/v1/operator-challenge HTTP/1.1\r\nHost: localhost\r\n'
        if phase == 'headers':prefix += b'X-Incomplete: '
        else:prefix += b'Content-Type: application/json\r\nContent-Length: 100\r\n\r\n{'
        connection.sendall(prefix)
        stop_drip = Event()
        def drip():
            while not stop_drip.wait(0.05):
                try:connection.sendall(b' ')
                except OSError:return
        sender = Thread(target=drip, daemon=True)
        sender.start()
        received = bytearray()
        try:
            while True:
                chunk = connection.recv(4096)
                if not chunk:break
                received.extend(chunk)
        except (ssl.SSLError, ConnectionResetError):
            pass
        assert monotonic()-start < 2.5
        assert b'200 OK' not in received
        assert signatures == []
    finally:
        if 'stop_drip' in locals():stop_drip.set()
        connection.close()
        if 'sender' in locals():sender.join(timeout=1)
        server.shutdown();server.server_close()
        thread.join(timeout=3)
        assert not thread.is_alive()
