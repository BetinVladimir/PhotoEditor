#!/usr/bin/env python3
"""Create deterministic release evidence without reading outside the repository."""
from __future__ import annotations
import argparse,hashlib,json,subprocess
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
LOCKS=("apps/local-editor/requirements.lock","backend/go.mod","backend/go.sum","apps/portal/package-lock.json","apps/photographer/package-lock.json","configs/capacity-rtx3060.json")

def digest(path:Path)->str:
 h=hashlib.sha256()
 with path.open("rb") as stream:
  for block in iter(lambda:stream.read(1024*1024),b""):h.update(block)
 return h.hexdigest()

def git(*args:str)->str:
 return subprocess.run(["git",*args],cwd=ROOT,text=True,stdout=subprocess.PIPE,stderr=subprocess.DEVNULL,check=False).stdout.strip()

def python_components()->list[dict]:
 out=[]
 for line in (ROOT/"apps/local-editor/requirements.lock").read_text().splitlines():
  line=line.strip()
  if not line or line.startswith("#") or line.startswith("-") or "==" not in line:continue
  name,version=line.split("==",1);version=version.split()[0].rstrip("\\")
  if name and version:out.append({"type":"library","name":name.lower(),"version":version,"purl":f"pkg:pypi/{name.lower()}@{version}"})
 return out

def npm_components(lock:Path)->list[dict]:
 data=json.loads(lock.read_text());out=[]
 for key,value in sorted(data.get("packages",{}).items()):
  if not key.startswith("node_modules/") or not value.get("version"):continue
  name=key.removeprefix("node_modules/");version=value["version"]
  out.append({"type":"library","name":name,"version":version,"purl":f"pkg:npm/{name.replace('@','%40')}@{version}"})
 return out

def go_components()->list[dict]:
 out=[];inside=False
 for raw in (ROOT/"backend/go.mod").read_text().splitlines():
  line=raw.split("//",1)[0].strip()
  if line=="require (":inside=True;continue
  if inside and line==")":inside=False;continue
  if line.startswith("require "):line=line.removeprefix("require ").strip()
  elif not inside:continue
  fields=line.split()
  if len(fields)!=2 or not fields[1].startswith("v"):continue
  name,version=fields;out.append({"type":"library","name":name,"version":version,"purl":f"pkg:golang/{name}@{version}"})
 return out

def build()->tuple[dict,dict]:
 locks=[]
 for rel in LOCKS:
  path=ROOT/rel
  if not path.is_file():raise SystemExit(f"missing lockfile: {rel}")
  locks.append({"path":rel,"bytes":path.stat().st_size,"sha256":digest(path)})
 components=python_components()+go_components()
 for rel in ("apps/portal/package-lock.json","apps/photographer/package-lock.json"):components+=npm_components(ROOT/rel)
 unique={(x["purl"]):x for x in components}
 sbom={"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"metadata":{"component":{"type":"application","name":"PhotoEditor","version":git("rev-parse","--short=12","HEAD") or "working-tree"}},"components":[unique[k] for k in sorted(unique)]}
 manifest={"schema_version":1,"source_commit":git("rev-parse","HEAD") or None,"dirty":bool(git("status","--porcelain")),"lockfiles":locks,"sbom_sha256":hashlib.sha256((json.dumps(sbom,sort_keys=True,separators=(",",":"))+"\n").encode()).hexdigest()}
 return manifest,sbom

def main()->None:
 parser=argparse.ArgumentParser();parser.add_argument("--check",action="store_true");args=parser.parse_args();manifest,sbom=build();out=ROOT/"release";expected={out/"manifest.json":manifest,out/"sbom.cdx.json":sbom}
 if args.check:
  for path,value in expected.items():
   if not path.is_file() or json.loads(path.read_text())!=value:raise SystemExit(f"release evidence drift: {path.relative_to(ROOT)}")
 else:
  out.mkdir(exist_ok=True)
  for path,value in expected.items():path.write_text(json.dumps(value,ensure_ascii=False,sort_keys=True,indent=2)+"\n")
 print(json.dumps({"components":len(sbom["components"]),"lockfiles":len(manifest["lockfiles"]),"checked":args.check}))
if __name__=="__main__":main()
