"""Ed25519 seed stored only in a supported operating-system keychain."""

import base64
import uuid


class DeviceKey:
    def __init__(self, device_id: str):
        import keyring
        from cryptography.hazmat.primitives.asymmetric.ed25519 import Ed25519PrivateKey
        self.private_type = Ed25519PrivateKey
        self.account = str(uuid.UUID(device_id))
        backend = keyring.get_keyring()
        if type(backend).__module__ not in {
            "keyring.backends.macOS", "keyring.backends.Windows",
            "keyring.backends.SecretService", "keyring.backends.libsecret",
        }:
            raise RuntimeError("a supported OS keychain is required")
        # Keep the validated backend, rather than resolving global keyring
        # routing again when reading or writing sensitive material.
        self.keyring = backend

    def load(self):
        encoded = self.keyring.get_password("PhotoEditor studio-agent Ed25519", self.account)
        if encoded is None:
            raise RuntimeError("device key is not provisioned")
        seed = base64.b64decode(encoded, validate=True)
        if len(seed) != 32:
            raise RuntimeError("invalid device key")
        return self.private_type.from_private_bytes(seed)

    def create(self) -> str:
        from cryptography.hazmat.primitives import serialization
        if self.keyring.get_password("PhotoEditor studio-agent Ed25519", self.account) is not None:
            raise RuntimeError("device key already exists")
        private = self.private_type.generate()
        seed = private.private_bytes(serialization.Encoding.Raw, serialization.PrivateFormat.Raw,
                                     serialization.NoEncryption())
        self.keyring.set_password("PhotoEditor studio-agent Ed25519", self.account,
                                 base64.b64encode(seed).decode())
        return self.public_key()

    def public_key(self) -> str:
        from cryptography.hazmat.primitives import serialization
        raw = self.load().public_key().public_bytes(serialization.Encoding.Raw, serialization.PublicFormat.Raw)
        return base64.urlsafe_b64encode(raw).rstrip(b"=").decode()

    def sign(self, payload: bytes) -> bytes:
        return self.load().sign(payload)
