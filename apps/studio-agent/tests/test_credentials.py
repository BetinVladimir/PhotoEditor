import base64
import sys
import types
import unittest
import uuid
from unittest.mock import Mock, patch

from studio_agent.credentials import DeviceKey


class KeychainBindingTest(unittest.TestCase):
    def backend(self, module="keyring.backends.macOS", encoded=None):
        backend_type = type("Backend", (), {"__module__": module})
        backend = backend_type()
        backend.get_password = Mock(return_value=encoded)
        backend.set_password = Mock()
        return backend

    def test_reads_and_signs_only_with_validated_backend(self):
        backend = self.backend(encoded=base64.b64encode(bytes(32)).decode())
        router = types.SimpleNamespace(get_keyring=Mock(return_value=backend),
            get_password=Mock(side_effect=AssertionError("global keyring read")),
            set_password=Mock(side_effect=AssertionError("global keyring write")))
        with patch.dict(sys.modules, {"keyring": router}):
            key = DeviceKey(str(uuid.uuid4()))
            router.get_keyring.return_value = self.backend("keyring.backends.fail")
            public = key.public_key()
            signature = key.sign(b"capture challenge")
        self.assertEqual(len(base64.urlsafe_b64decode(public + "=")), 32)
        self.assertEqual(len(signature), 64)
        self.assertEqual(router.get_keyring.call_count, 1)
        self.assertEqual(backend.get_password.call_count, 2)
        router.get_password.assert_not_called()
        router.set_password.assert_not_called()

    def test_create_writes_and_reads_back_same_validated_backend(self):
        backend = self.backend()
        stored = {}
        backend.get_password.side_effect = lambda service, account: stored.get((service, account))
        backend.set_password.side_effect = lambda service, account, value: stored.__setitem__((service, account), value)
        router = types.SimpleNamespace(get_keyring=Mock(return_value=backend),
            get_password=Mock(side_effect=AssertionError("global keyring read")),
            set_password=Mock(side_effect=AssertionError("global keyring write")))
        device = str(uuid.uuid4())
        with patch.dict(sys.modules, {"keyring": router}):
            key = DeviceKey(device)
            public = key.create()
            self.assertEqual(key.public_key(), public)
            with self.assertRaisesRegex(RuntimeError, "already exists"):
                key.create()
        self.assertEqual(len(stored), 1)
        self.assertEqual(len(base64.b64decode(next(iter(stored.values())), validate=True)), 32)
        self.assertEqual(backend.set_password.call_count, 1)
        router.get_password.assert_not_called()
        router.set_password.assert_not_called()

    def test_unapproved_backend_never_reads_or_writes_seed(self):
        backend = self.backend("keyrings.alt.file")
        router = types.SimpleNamespace(get_keyring=Mock(return_value=backend))
        with patch.dict(sys.modules, {"keyring": router}):
            with self.assertRaisesRegex(RuntimeError, "supported OS keychain"):
                DeviceKey(str(uuid.uuid4()))
        backend.get_password.assert_not_called()
        backend.set_password.assert_not_called()


if __name__ == "__main__":
    unittest.main()
