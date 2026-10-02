import importlib.util,json
from pathlib import Path

ROOT=Path(__file__).resolve().parents[2]
spec=importlib.util.spec_from_file_location("release_manifest",ROOT/"scripts/release_manifest.py")
module=importlib.util.module_from_spec(spec);spec.loader.exec_module(module)

def test_release_evidence_is_deterministic_and_complete():
 first=module.build();second=module.build();assert first==second
 manifest,sbom=first
 assert manifest["schema_version"]==1 and len(manifest["lockfiles"])==6
 assert all(len(x["sha256"])==64 and x["bytes"]>0 for x in manifest["lockfiles"])
 purls=[x["purl"] for x in sbom["components"]]
 assert purls==sorted(set(purls)) and any(x.startswith("pkg:golang/") for x in purls)
 assert any(x.startswith("pkg:npm/") for x in purls) and any(x.startswith("pkg:pypi/") for x in purls)

def test_go_inventory_reads_lockfile_without_running_go(monkeypatch):
 monkeypatch.setattr(module.subprocess,"run",lambda *a,**k:(_ for _ in ()).throw(AssertionError("must stay offline")))
 assert module.go_components() and all(x["purl"].startswith("pkg:golang/") for x in module.go_components())
