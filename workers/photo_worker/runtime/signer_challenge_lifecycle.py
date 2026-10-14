"""Owned challenge listener lifecycle for explicit signer process composition."""
from contextlib import contextmanager
from dataclasses import dataclass
from threading import Event, Thread
import re
import ssl

from .signer_challenge_http import build_signer_challenge_server


@contextmanager
def running_signer_challenges(address, application, tls_context, *, timeout=10,
                              concurrency=4, check_active=lambda: None):
    """Yield a live guard; the worker must compose it into its native guard.

    Configuration and bind errors occur before yielding. Listener termination
    invalidates the guard, including clean but unexpected serve_forever exit.
    Exiting the worker scope always closes the listener and active sockets.
    This does not manufacture native or SQL qualification.
    """
    if not callable(check_active):
        raise ValueError('SIGNER_CHALLENGE_LIFECYCLE_GUARD')
    if check_active() is False:
        raise RuntimeError('SIGNER_TASK_INACTIVE')
    server = build_signer_challenge_server(address, application, tls_context,
                                            timeout=timeout, concurrency=concurrency)
    started, exited, stopping = Event(), Event(), Event()
    def serve():
        started.set()
        try:
            server.serve_forever(poll_interval=0.05)
        except Exception:
            pass  # Guard exposes failure without leaking native/TLS details.
        finally:
            exited.set()
            server.server_close()
    thread = Thread(target=serve, name='photo-signer-challenge', daemon=True)
    def active():
        if check_active() is False:
            raise RuntimeError('SIGNER_TASK_INACTIVE')
        if stopping.is_set() or exited.is_set() or not thread.is_alive() or server.stopping.is_set():
            raise RuntimeError('SIGNER_CHALLENGE_LISTENER_STOPPED')
        server.verify_tls()
        if stopping.is_set() or exited.is_set() or not thread.is_alive() or server.stopping.is_set():
            raise RuntimeError('SIGNER_CHALLENGE_LISTENER_STOPPED')
    try:
        thread.start()
        if not started.wait(timeout=1):
            raise RuntimeError('SIGNER_CHALLENGE_LISTENER_START')
        active()
        yield active
    finally:
        stopping.set()
        if thread.is_alive() and not exited.is_set():server.shutdown()
        server.server_close()
        if thread.ident is not None:thread.join(timeout=2)
        if thread.is_alive():
            raise RuntimeError('SIGNER_CHALLENGE_LISTENER_STOP')


@dataclass(frozen=True)
class SigningChallengeListenerInstallation:
    address: tuple
    tls_context: object
    client_certificate_shas: tuple[str, ...]
    timeout: float = 10
    concurrency: int = 4

    def __post_init__(self):
        if (not isinstance(self.address, tuple) or len(self.address) != 2
                or not isinstance(self.address[0], str) or not 1 <= len(self.address[0]) <= 253
                or any(c.isspace() or ord(c) < 32 for c in self.address[0])
                or type(self.address[1]) is not int or not 0 <= self.address[1] <= 65535
                or not isinstance(self.tls_context, ssl.SSLContext)
                or self.tls_context.protocol != ssl.PROTOCOL_TLS_SERVER
                or self.tls_context.verify_mode != ssl.CERT_REQUIRED
                or self.tls_context.minimum_version < ssl.TLSVersion.TLSv1_3
                or type(self.timeout) not in (int, float) or not 1 <= self.timeout <= 30
                or type(self.concurrency) is not int or not 1 <= self.concurrency <= 16
                or not isinstance(self.client_certificate_shas, (list, tuple))
                or not 1 <= len(self.client_certificate_shas) <= 64
                or any(not isinstance(pin, str) or re.fullmatch('[0-9a-f]{64}', pin) is None
                       for pin in self.client_certificate_shas)
                or len(set(self.client_certificate_shas)) != len(self.client_certificate_shas)):
            raise ValueError('SIGNER_CHALLENGE_LISTENER_INSTALLATION')
        object.__setattr__(self, 'client_certificate_shas', tuple(self.client_certificate_shas))

    def running(self, policies, operator, *, check_active):
        from .signer_challenge_http import SigningChallengeApplication
        application = SigningChallengeApplication(policies, operator, self.client_certificate_shas)
        return running_signer_challenges(self.address, application, self.tls_context,
            timeout=self.timeout, concurrency=self.concurrency, check_active=check_active)
