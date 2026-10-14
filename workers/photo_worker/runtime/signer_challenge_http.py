"""Private, pinned-mTLS operator possession and native checks; no image/task signing API."""
from hashlib import sha256
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from http.client import LineTooLong
import re
import ssl
import socket
from threading import BoundedSemaphore, Event, Lock, Timer
from time import monotonic

from photo_worker.contracts.base import canonical_bytes, parse_json
from photo_worker.contracts.provenance import SigningPolicy, decode_provenance
from photo_worker.operations.provenance.installation import InstalledSigningOperator
from photo_worker.operations.provenance.cancellation import signing_task_scope, check_signing_active


class SigningChallengeApplication:
    def __init__(self, policies, operator, client_certificate_shas):
        if (not isinstance(operator, InstalledSigningOperator)
                or not isinstance(policies, (list, tuple)) or not 1 <= len(policies) <= 256
                or any(not isinstance(raw, bytes) for raw in policies)
                or sum(map(len, policies)) > 8 << 20
                or not isinstance(client_certificate_shas, (list, tuple, set, frozenset))
                or not 1 <= len(client_certificate_shas) <= 64
                or any(not isinstance(pin, str) or re.fullmatch('[0-9a-f]{64}', pin) is None
                       for pin in client_certificate_shas)):
            raise ValueError('SIGNER_CHALLENGE_INSTALLATION')
        self._policies = {}
        for raw in policies:
            decode_provenance(raw, SigningPolicy, 1 << 20)
            pin = sha256(raw).hexdigest()
            if pin in self._policies:
                raise ValueError('SIGNER_CHALLENGE_DUPLICATE')
            self._policies[pin] = bytes(raw)
        self._clients, self._operator = frozenset(client_certificate_shas), operator

    def handle(self, raw, peer_certificate):
        # Chain verification belongs to the mandatory server SSLContext. This
        # second gate restricts the exact API leaf identity after TLS succeeds.
        check_signing_active()
        if (not isinstance(peer_certificate, bytes) or not 1 <= len(peer_certificate) <= 1 << 20
                or sha256(peer_certificate).hexdigest() not in self._clients):
            raise PermissionError('SIGNER_CHALLENGE_CLIENT')
        if not isinstance(raw, bytes) or not 2 <= len(raw) <= 9216:
            raise ValueError('SIGNER_CHALLENGE_REQUEST')
        request = parse_json(raw, max_bytes=9216)
        if (not isinstance(request, dict)
                or set(request) != {'schema_version', 'key_handle', 'algorithm', 'payload_hex'}
                or type(request['schema_version']) is not int or request['schema_version'] != 1
                or canonical_bytes(request) != raw
                or not isinstance(request['payload_hex'], str)
                or not 4 <= len(request['payload_hex']) <= 8192
                or len(request['payload_hex']) % 2
                or re.fullmatch('[0-9a-f]+', request['payload_hex']) is None):
            raise ValueError('SIGNER_CHALLENGE_REQUEST')
        payload = bytes.fromhex(request['payload_hex'])
        challenge = parse_json(payload, max_bytes=4096)
        if not isinstance(challenge, dict) or not isinstance(challenge.get('policy_sha256'), str):
            raise ValueError('SIGNER_CHALLENGE_POLICY')
        installed = self._policies.get(challenge['policy_sha256'])
        if installed is None:
            raise ValueError('SIGNER_CHALLENGE_POLICY')
        policy = decode_provenance(installed, SigningPolicy, 1 << 20)
        check_signing_active()
        signature = self._operator.sign_operator_challenge(
            policy, request['key_handle'], request['algorithm'], payload)
        check_signing_active()
        return canonical_bytes({'schema_version': 1, 'signature_hex': signature.hex()})

    def check_native(self, raw, peer_certificate):
        """Check the installed SDK/key against one pinned policy over the same mTLS boundary."""
        check_signing_active()
        if (not isinstance(peer_certificate, bytes) or not 1 <= len(peer_certificate) <= 1 << 20
                or sha256(peer_certificate).hexdigest() not in self._clients):
            raise PermissionError('SIGNER_CHALLENGE_CLIENT')
        if not isinstance(raw, bytes) or not 2 <= len(raw) <= 9216:
            raise ValueError('SIGNER_NATIVE_CHECK_REQUEST')
        request = parse_json(raw, max_bytes=9216)
        if (not isinstance(request, dict)
                or set(request) != {'schema_version', 'policy_sha256', 'certificate_chain_sha256'}
                or type(request['schema_version']) is not int or request['schema_version'] != 1
                or canonical_bytes(request) != raw
                or any(not isinstance(request[key], str) or re.fullmatch('[0-9a-f]{64}', request[key]) is None
                       for key in ('policy_sha256', 'certificate_chain_sha256'))):
            raise ValueError('SIGNER_NATIVE_CHECK_REQUEST')
        installed = self._policies.get(request['policy_sha256'])
        if installed is None:
            raise ValueError('SIGNER_NATIVE_CHECK_POLICY')
        policy = decode_provenance(installed, SigningPolicy, 1 << 20)
        if policy.certificate_chain_sha != request['certificate_chain_sha256']:
            raise ValueError('SIGNER_NATIVE_CHECK_CERTIFICATE')
        self._operator.check_installation(policy)
        check_signing_active()
        return canonical_bytes({'schema_version': 1, 'policy_sha256': request['policy_sha256'],
                                'certificate_chain_sha256': request['certificate_chain_sha256']})


def build_signer_challenge_server(address, application, tls_context, *, timeout=10, concurrency=4):
    if (not isinstance(application, SigningChallengeApplication)
            or not isinstance(tls_context, ssl.SSLContext)
            or tls_context.protocol != ssl.PROTOCOL_TLS_SERVER
            or tls_context.verify_mode != ssl.CERT_REQUIRED
            or tls_context.minimum_version < ssl.TLSVersion.TLSv1_3
            or type(timeout) not in (int, float) or not 1 <= timeout <= 30
            or type(concurrency) is not int or not 1 <= concurrency <= 16):
        raise ValueError('SIGNER_CHALLENGE_TLS_INSTALLATION')

    def verify_tls():
        if (tls_context.protocol != ssl.PROTOCOL_TLS_SERVER
                or tls_context.verify_mode != ssl.CERT_REQUIRED
                or tls_context.minimum_version < ssl.TLSVersion.TLSv1_3):
            raise ValueError('SIGNER_CHALLENGE_TLS_INSTALLATION_CHANGED')

    class HeaderBoundedReader:
        def __init__(self, reader):self.reader, self.remaining = reader, 8192
        def readline(self, maximum=-1):
            limit = self.remaining + 1
            if maximum >= 0:limit = min(limit, maximum)
            line = self.reader.readline(limit)
            self.remaining -= len(line)
            if self.remaining < 0:raise LineTooLong('signer request headers')
            return line
        def read(self, count=-1):return self.reader.read(count)
        def close(self):self.reader.close()

    class Handler(BaseHTTPRequestHandler):
        server_version = 'PhotoSigner'
        sys_version = ''
        def setup(self):
            super().setup()
            self.rfile = HeaderBoundedReader(self.rfile)
            self.request_deadline = monotonic() + timeout
            self.expired = Event()
            def expire():
                self.expired.set()
                # A wall-clock bound, independent of repeated partial reads.
                try:self.connection.shutdown(socket.SHUT_RDWR)
                except OSError:pass
                self.connection.close()
            self.deadline_timer = Timer(timeout, expire)
            self.deadline_timer.daemon = True
            self.deadline_timer.start()
        def finish(self):
            try:super().finish()
            finally:self.deadline_timer.cancel()
        def log_message(self, *_args):
            pass  # Never log key handles, challenges, certificates or exceptions.
        def do_POST(self):
            self.close_connection = True
            if (self.path not in ('/internal/v1/operator-challenge', '/internal/v1/operator-native-check')
                    or self.headers.get_all('Transfer-Encoding') is not None
                    or self.headers.get_all('Expect') is not None
                    or self.headers.get_all('Content-Encoding') is not None
                    or self.headers.get_all('Content-Type') != ['application/json']
                    or len(self.headers.get_all('Content-Length', [])) != 1):
                self.send_error(400);return
            length = self.headers['Content-Length']
            if re.fullmatch('[1-9][0-9]{0,4}', length) is None or not 2 <= int(length) <= 9216:
                self.send_error(400);return
            def active():
                if self.server.stopping.is_set():
                    raise RuntimeError('SIGNER_CHALLENGE_STOPPED')
                if self.expired.is_set() or monotonic() >= self.request_deadline:
                    raise TimeoutError('SIGNER_CHALLENGE_TIMEOUT')
            try:
                raw = self.rfile.read(int(length))
                if len(raw) != int(length):raise ValueError('SIGNER_CHALLENGE_REQUEST')
                with signing_task_scope(active):
                    peer = self.connection.getpeercert(binary_form=True)
                    response = (application.handle(raw, peer) if self.path == '/internal/v1/operator-challenge'
                                else application.check_native(raw, peer))
                active()
            except PermissionError:
                self.send_error(403);return
            except Exception:
                self.send_error(503);return
            self.send_response(200)
            self.send_header('Content-Type', 'application/json')
            self.send_header('Content-Length', str(len(response)))
            self.send_header('Cache-Control', 'no-store')
            self.send_header('Connection', 'close')
            self.end_headers();self.wfile.write(response)

    class Server(ThreadingHTTPServer):
        daemon_threads = True
        def get_request(self):
            sock, peer = self.socket.accept();sock.settimeout(timeout)
            # Admission and TLS handshake run in the bounded request thread.
            # A peer that sends no ClientHello must not block the accept loop.
            return sock, peer
        def process_request(self, request, client_address):
            with self.connections_lock:
                if self.stopping.is_set() or not self.slots.acquire(blocking=False):
                    self.shutdown_request(request);return
                self.connections.add(request)
            try:
                super().process_request(request, client_address)
            except Exception:
                with self.connections_lock:self.connections.discard(request)
                self.slots.release()
                raise
        def process_request_thread(self, request, client_address):
            secured = None
            handshake_timer = None
            handshake_expired = Event()
            try:
                verify_tls()
                # Replace the tracked raw socket atomically: wrap_socket takes
                # ownership of its descriptor even before handshake completes.
                with self.connections_lock:
                    if self.stopping.is_set():return
                    secured = tls_context.wrap_socket(request, server_side=True,
                                                      do_handshake_on_connect=False)
                    self.connections.discard(request)
                    self.connections.add(secured)
                # Socket inactivity timeout alone permits a peer to retain a
                # slot by sending TLS records slowly. Bound total handshake time.
                def expire_handshake():
                    handshake_expired.set()
                    try:secured.shutdown(socket.SHUT_RDWR)
                    except OSError:pass
                    secured.close()
                handshake_timer = Timer(timeout, expire_handshake)
                handshake_timer.daemon = True
                handshake_timer.start()
                secured.do_handshake()
                handshake_timer.cancel()
                if handshake_expired.is_set():return
                verify_tls()
                if self.stopping.is_set():return
                super().process_request_thread(secured, client_address)
            except Exception:
                pass  # Never expose TLS/native exception details.
            finally:
                if handshake_timer is not None:handshake_timer.cancel()
                self.shutdown_request(secured if secured is not None else request)
                with self.connections_lock:
                    self.connections.discard(request)
                    if secured is not None:self.connections.discard(secured)
                self.slots.release()
        def stop_connections(self):
            self.stopping.set()
            with self.connections_lock:
                for connection in tuple(self.connections):
                    try:connection.shutdown(socket.SHUT_RDWR)
                    except OSError:pass
                    connection.close()
        def shutdown(self):
            self.stop_connections()
            super().shutdown()
        def server_close(self):
            self.stop_connections()
            super().server_close()
        def handle_error(self, request, client_address):
            pass
    server = Server(address, Handler, bind_and_activate=False)
    server.slots = BoundedSemaphore(concurrency)
    server.verify_tls = verify_tls
    server.stopping = Event()
    server.connections_lock = Lock()
    server.connections = set()
    try:server.server_bind();server.server_activate()
    except Exception:server.server_close();raise
    return server
