import json
import tempfile
import unittest
from pathlib import Path
from unittest.mock import patch

from studio_agent.config import load_config
from studio_agent.cli import main
from studio_agent.raw_policy import canonical_raw_policy


class ConfigTest(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.path = Path(self.temp.name) / 'agent.json'
        self.config = {field: '12345678-1234-4234-8234-123456789abc'
                       for field in ('key_id', 'device_id', 'tenant_id', 'project_id',
                                     'session_id', 'collection_id')}
        self.config.update(api_origin='https://api.example.test',
                           storage_origins=['https://storage.example.test'],
                           watch_root='/camera', journal_path='/journal.sqlite',
                           ca_bundle='/operator.pem')

    def write(self, config):
        self.path.write_text(json.dumps(config))

    def test_defaults_and_explicit_loopback_development(self):
        self.write(self.config)
        loaded = load_config(self.path)
        self.assertEqual(loaded['max_queued_bytes'], 100 * 1024**3)
        self.assertIs(loaded['development'], False)
        self.config.update(development=True, api_origin='http://127.0.0.1:8080')
        del self.config['ca_bundle']
        self.write(self.config)
        self.assertEqual(load_config(self.path)['api_origin'], 'http://127.0.0.1:8080')

    def test_explicit_raw_policy_is_closed_and_canonical_before_keychain(self):
        policy = {"schema_version": 1, "demosaic": "ahd", "wb_source": "camera",
                  "exposure_ev": 0.5, "highlight_policy": "blend",
                  "decoder_version": "rawpy-0.27.1/libraw-0.21.4"}
        self.write({**self.config, "raw_development": policy})
        self.assertEqual(load_config(self.path)["raw_development"], canonical_raw_policy(policy))
        for changed in ({**policy, "decoder_version": "unqualified"},
                        {**policy, "exposure_ev": True},
                        {**policy, "exposure_ev": 4},
                        {**policy, "extra": "field"}):
            with self.subTest(changed=changed):
                self.write({**self.config, "raw_development": changed})
                with self.assertRaises(ValueError):
                    load_config(self.path)

    def test_invalid_fields_scope_paths_and_limits(self):
        for change in ({'max_queued_bytes': True}, {'max_queued_bytes': 1.5},
                       {'max_queued_bytes': 0}, {'max_queued_bytes': 2**53},
                       {'storage_origins': 'https://storage.example.test'},
                       {'storage_origins': []}, {'storage_origins': [None]},
                       {'development': 1}, {'watch_root': ''}, {'journal_path': '\x00'},
                       {'ca_bundle': None}, {'device_id': '00000000-0000-0000-0000-000000000000'},
                       {'key_id': 'bad'}, {'access_token': 'secret'},
                       {'api_origin': 'http://external.example.test'}):
            with self.subTest(change=change):
                self.write({**self.config, **change})
                with self.assertRaises(ValueError):
                    load_config(self.path)

    def test_malformed_duplicate_nonfinite_and_oversized_json(self):
        for raw in ('[]', '{}', '{"x":1,"x":2}', '{"x":NaN}',
                    '{"x":Infinity}', '{', ' ' * 65537):
            with self.subTest(raw=raw[:40]):
                self.path.write_text(raw)
                with self.assertRaises(ValueError):
                    load_config(self.path)

    def test_invalid_configuration_fails_before_keychain_or_transport(self):
        self.write({**self.config, 'max_queued_bytes': False})
        with patch('sys.argv', ['photo-studio-agent', 'run', '--config', str(self.path)]), \
             patch('studio_agent.cli.DeviceKey') as key, \
             patch('studio_agent.cli.HTTPClient') as client:
            with self.assertRaises(ValueError):
                main()
            key.assert_not_called()
            client.assert_not_called()


class AgentCleanupTest(unittest.TestCase):
    def test_journal_closes_and_lock_releases_even_if_disconnect_fails(self):
        config = {"development": True, "key_id": "key", "api_origin": "http://localhost",
                  "tenant_id": "tenant", "project_id": "project", "session_id": "session",
                  "collection_id": "collection", "device_id": "device",
                  "storage_origins": ["http://localhost"], "journal_path": "/unused.sqlite",
                  "watch_root": "/unused-camera"}
        for tick_error in (None, KeyboardInterrupt(), RuntimeError("tick failed")):
            with self.subTest(tick_error=type(tick_error).__name__), \
                 patch("sys.argv", ["photo-studio-agent", "run", "--config", "/unused.json", "--once"]), \
                 patch("studio_agent.cli.load_config", return_value=config), \
                 patch("studio_agent.cli.DeviceKey"), \
                 patch("studio_agent.cli.HTTPClient"), \
                 patch("studio_agent.cli.JournalLock") as lock_type, \
                 patch("studio_agent.cli.Journal") as journal_type, \
                 patch("studio_agent.cli.WatchFolder"), \
                 patch("studio_agent.cli.Transfer"), \
                 patch("studio_agent.cli.Runner") as runner_type:
                runner = runner_type.return_value
                runner.tick.side_effect = tick_error
                runner.camera.disconnect.side_effect = RuntimeError("disconnect failed")
                with self.assertRaisesRegex(RuntimeError, "disconnect failed"):
                    main()
                journal_type.return_value.close.assert_called_once_with()
                runner.camera.disconnect.assert_called_once_with()
                lock_type.return_value.__exit__.assert_called_once()
