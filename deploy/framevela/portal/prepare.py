#!/usr/bin/env python3
"""Prepare an isolated portal build; refuse to overwrite an existing directory."""
import pathlib,shutil,sys
repo=pathlib.Path(__file__).resolve().parents[3]
target=pathlib.Path(sys.argv[1]).resolve()
if repo not in target.parents:raise SystemExit("Build destination must be inside this repository")
target.mkdir(parents=True,exist_ok=False)
for name in ["src","public"]:
 source=repo/"apps/portal"/name
 if source.exists():shutil.copytree(source,target/name)
for name in ["package.json","package-lock.json","vite.config.ts","wrangler.jsonc","tsconfig.json","portal-worker.mjs"]:
 source=pathlib.Path(__file__).parent/name
 if source.exists():shutil.copy2(source,target/name)
(target/".env.production").write_text("NEXT_PUBLIC_API_ORIGIN=https://api.framevela.com\n")
print(target)
