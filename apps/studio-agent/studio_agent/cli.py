"""Studio-agent command line; secrets are never read from config or printed."""

import argparse
import json
import time
import uuid
from pathlib import Path

from .config import load_config
from .credentials import DeviceKey
from .http import HTTPClient
from .journal import Journal
from .process_lock import JournalLock
from .runner import Runner
from .transfer import Transfer
from .watchfolder import WatchFolder


def main() -> int:
    parser = argparse.ArgumentParser(prog="photo-studio-agent")
    commands = parser.add_subparsers(dest="command", required=True)
    create = commands.add_parser("create-key")
    create.add_argument("--key-id", default=None)
    run = commands.add_parser("run")
    run.add_argument("--config", type=Path, required=True)
    run.add_argument("--once", action="store_true")
    status = commands.add_parser("status")
    status.add_argument("--journal", type=Path, required=True)
    status.add_argument("--device-id", required=True)
    retry = commands.add_parser("retry")
    retry.add_argument("--journal", type=Path, required=True)
    retry.add_argument("--device-id", required=True)
    retry.add_argument("--watch-root", type=Path, required=True)
    retry.add_argument("--sequence", type=int, required=True)
    args = parser.parse_args()
    if args.command == "retry":
        device_id = str(uuid.UUID(args.device_id))
        if args.sequence < 1 or not args.journal.is_file():
            raise ValueError("existing journal and positive sequence are required")
        with JournalLock(args.journal):
            journal = Journal(args.journal)
            try:
                entry = WatchFolder(args.watch_root, journal, device_id).retry_failed(args.sequence)
                print(json.dumps({"device_id": entry.device_id, "sequence": str(entry.sequence), "state": entry.state}))
            finally:
                journal.close()
            return 0
    if args.command == "status":
        device_id = str(uuid.UUID(args.device_id))
        if not args.journal.is_file():
            raise ValueError("journal does not exist")
        journal = Journal(args.journal, readonly=True)
        try:
            print(json.dumps(journal.summary(device_id), allow_nan=False))
        finally:
            journal.close()
        return 0
    if args.command == "create-key":
        key_id = args.key_id or str(uuid.uuid4())
        print(json.dumps({"key_id": key_id, "public_key": DeviceKey(key_id).create()}))
        return 0
    config = load_config(args.config)
    development = config["development"]
    key = DeviceKey(config["key_id"])
    key.load()
    client = HTTPClient(config["api_origin"], config["tenant_id"], config["project_id"],
        config["session_id"], config["collection_id"], config["device_id"], key.sign,
        config["storage_origins"], server_origin=config.get("server_origin"),
        development=development, ca_bundle=config.get("ca_bundle"))
    with JournalLock(Path(config["journal_path"])):
        journal = Journal(Path(config["journal_path"]))
        runner = None
        try:
            watch = WatchFolder(Path(config["watch_root"]), journal, client.device_id,
                max_file_bytes=512 * 1024**2,
                max_queued_bytes=config.get("max_queued_bytes", 100 * 1024**3),
                raw_policy_json=config.get("raw_development"))
            journal.recover(client.device_id)
            runner = Runner(Transfer(watch, client))
            while True:
                runner.tick()
                if args.once:
                    return 0
                time.sleep(1)
        except KeyboardInterrupt:
            return 0
        finally:
            try:
                if runner is not None:
                    runner.camera.disconnect()
            finally:
                journal.close()


if __name__ == "__main__":
    raise SystemExit(main())
