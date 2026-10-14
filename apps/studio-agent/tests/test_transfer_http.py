"""Real local HTTP and file IO; server state is a fixture, not SQL/ingest/QA."""
import hashlib
import json
import tempfile
import threading
import time
import unittest
import uuid
from datetime import datetime, timedelta, timezone
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer
from pathlib import Path

from studio_agent.http import HTTPClient
from studio_agent.journal import Journal
from studio_agent.transfer import Transfer
from studio_agent.watchfolder import WatchFolder


class TransferHTTPTest(unittest.TestCase):
    def test_lost_complete_reply_recovers_without_reuploading_original(self):
        ids = [str(uuid.uuid4()) for _ in range(5)]
        upload = str(uuid.uuid4())
        receipt = str(uuid.uuid4())
        original = b'camera original bytes'
        sha = hashlib.sha256(original).hexdigest()
        state = {'phase': 'uploading', 'puts': [], 'keys': [], 'complete': None}
        failures = []

        class Handler(BaseHTTPRequestHandler):
            def log_message(self, *args):
                pass

            def reply(self, body):
                data = json.dumps(body).encode()
                self.send_response(200)
                self.send_header('Content-Type', 'application/json')
                self.send_header('Content-Length', str(len(data)))
                self.end_headers()
                self.wfile.write(data)

            def do_GET(self):
                if self.headers.get('Authorization') != 'Bearer fixture-token':
                    failures.append('missing API authorization')
                self.reply({'id': upload, 'state': state['phase'], 'part_bytes': 16,
                            'parts_total': 2, 'project_id': ids[1], 'collection_id': ids[3],
                            'expected_size': len(original)})

            def do_POST(self):
                body = json.loads(self.rfile.read(int(self.headers['Content-Length'])))
                if self.headers.get('Authorization') != 'Bearer fixture-token':
                    failures.append('missing API authorization')
                if self.path.endswith('/parts'):
                    number = body['part_numbers'][0]
                    self.reply({'upload_id': upload, 'items': [{'part_no': number,
                                'url': endpoint + '/storage/' + str(number),
                                'expires_at': (datetime.now(timezone.utc) +
                                               timedelta(minutes=10)).isoformat()}]})
                elif self.path.endswith('/complete'):
                    state['complete'] = body
                    state['keys'].append(self.headers.get('Idempotency-Key'))
                    state['phase'] = 'verified'
                    # The commit is accepted, but no HTTP status/body is sent.
                    self.close_connection = True
                elif self.path.endswith('/capture-receipts'):
                    state['keys'].append(self.headers.get('Idempotency-Key'))
                    if body['content_sha'] != sha or body['upload_id'] != upload:
                        failures.append('receipt source mismatch')
                    self.reply({'id': receipt, 'resource_type': 'capture_receipts',
                                'tenant_id': ids[0], 'project_id': ids[1], 'session_id': ids[2],
                                'device_id': ids[4], 'sequence': '1', 'content_sha': sha,
                                'upload_id': upload})
                else:
                    state['keys'].append(self.headers.get('Idempotency-Key'))
                    self.do_GET()

            def do_PUT(self):
                if self.headers.get('Authorization') or self.headers.get('Idempotency-Key'):
                    failures.append('API credentials leaked to storage')
                number = int(self.path.rsplit('/', 1)[1])
                data = self.rfile.read(int(self.headers['Content-Length']))
                state['puts'].append((number, data))
                self.send_response(200)
                self.send_header('ETag', 'etag-' + str(number))
                self.send_header('Content-Length', '0')
                self.end_headers()

        server = ThreadingHTTPServer(('127.0.0.1', 0), Handler)
        endpoint = 'http://127.0.0.1:' + str(server.server_port)
        thread = threading.Thread(target=server.serve_forever, daemon=True)
        thread.start()
        self.addCleanup(server.server_close)
        self.addCleanup(thread.join, 2)
        self.addCleanup(server.shutdown)
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory) / 'camera'
            root.mkdir()
            source = root / 'frame.jpg'
            source.write_bytes(original)
            journal_path = Path(directory) / 'journal.sqlite'
            journal = Journal(journal_path)
            try:
                watch = WatchFolder(root, journal, ids[4])
                watch.scan(1)
                watch.scan(2)
                client = HTTPClient(endpoint, *ids, lambda payload: bytes(64),
                                    [endpoint], development=True)
                # Credential challenge has a separate real-server fixture.
                client.token = 'fixture-token'
                client.token_until = time.monotonic() + 900
                with self.assertRaises(ConnectionError):
                    Transfer(watch, client).process(ids[4], 1)
                self.assertEqual(journal.get(ids[4], 1).state, 'uploading')
                journal.close()
                journal = Journal(journal_path)
                watch.journal = journal
                journal.recover(ids[4])
                result = Transfer(watch, client).process(ids[4], 1)
                self.assertEqual(result.state, 'done')
                self.assertEqual(result.upload_id, upload)
                self.assertEqual(sorted(state['puts']), [(1, original[:16]), (2, original[16:])])
                self.assertEqual(state['complete'], {'expected_size': len(original),
                    'parts': [{'part_no': 1, 'etag': 'etag-1'}, {'part_no': 2, 'etag': 'etag-2'}]})
                self.assertEqual(state['keys'], [f'capture/{ids[4]}/1/{action}'
                                                for action in ('begin', 'complete', 'receipt')])
                self.assertEqual(source.read_bytes(), original)
                self.assertEqual(failures, [])
            finally:
                journal.close()
