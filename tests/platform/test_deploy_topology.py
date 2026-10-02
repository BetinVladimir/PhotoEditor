from pathlib import Path
import re

import yaml


ROOT = Path(__file__).resolve().parents[2]
COMPOSE = ROOT / "deploy" / "compose.yaml"


def topology():
    value = yaml.safe_load(COMPOSE.read_text())
    assert value["name"] == "photoeditor"
    return value


def test_required_runtime_roles_are_deployed():
    services = topology()["services"]
    expected = {
        "postgres", "rabbitmq", "minio", "migrate", "bootstrap-storage",
        "bootstrap-queue", "api", "object-gateway", "scheduler", "v2-adopter", "publisher",
        "dead-letter-worker", "collection-worker", "maintenance", "mail-worker",
        "worker-media", "worker-cpu", "worker-archive", "worker-gpu",
        "photographer", "portal", "caddy",
    }
    assert expected == set(services)
    roles = {v.get("environment", {}).get("PHOTO_ROLE") for v in services.values()}
    assert {"migrate", "bootstrap-storage", "bootstrap-queue", "api", "object-gateway",
            "scheduler", "publisher", "dead-letter-worker", "collection-worker",
            "maintenance", "mail-worker"} <= roles


def test_images_are_mandatory_and_runtime_services_are_confined():
    data = topology()
    for name, service in data["services"].items():
        image = service["image"]
        assert ":?" in image, (name, image)
        if name not in {"postgres", "rabbitmq", "minio", "caddy"}:
            assert service.get("read_only") is True, name
            assert "no-new-privileges:true" in service["security_opt"]
            assert service["cap_drop"] == ["ALL"]
    assert data["networks"]["backend"]["internal"] is True
    assert set(data["services"]["caddy"]["networks"]) == {"edge", "backend"}


def test_workers_have_distinct_ids_fenced_gateway_and_bounded_scratch():
    services = topology()["services"]
    names = ("worker-media", "worker-cpu", "worker-archive", "worker-gpu")
    ids = set()
    for name in names:
        service = services[name]
        env = service["environment"]
        ids.add(env["PHOTO_WORKER_ID"])
        assert env["GATEWAY_ORIGIN"] == "https://object-gateway:8443"
        assert int(env["WORKER_MAX_SCRATCH_BYTES"]) > 0
        assert any(str(volume).endswith("/worker.key:ro") for volume in service["volumes"])
    assert len(ids) == len(names)
    assert services["worker-gpu"]["profiles"] == ["cuda"]
    assert services["mail-worker"]["profiles"] == ["mail"]


def test_one_shot_bootstrap_precedes_public_api():
    services = topology()["services"]
    api_dependencies = services["api"]["depends_on"]
    assert api_dependencies["migrate"]["condition"] == "service_completed_successfully"
    assert api_dependencies["bootstrap-storage"]["condition"] == "service_completed_successfully"
    assert api_dependencies["bootstrap-queue"]["condition"] == "service_completed_successfully"
    assert services["caddy"]["ports"] == ["80:80", "443:443"]
    assert services["caddy"]["depends_on"]["api"]["condition"] == "service_healthy"
    for name in ("api", "object-gateway", "scheduler", "publisher", "dead-letter-worker",
                 "collection-worker", "maintenance", "mail-worker", "worker-media",
                 "worker-cpu", "worker-archive", "worker-gpu"):
        assert "healthcheck" in services[name], name


def test_container_builds_require_explicit_base_images_and_nonroot_runtime():
    for name in ("api", "worker", "photographer", "portal"):
        raw = (ROOT / "deploy" / "images" / name / "Dockerfile").read_text()
        args = re.findall(r"^ARG ([A-Z][A-Z0-9_]*)$", raw, re.MULTILINE)
        assert args and all(f"FROM ${{{arg}}}" in raw for arg in args)
        assert "USER 10001:10001" in raw


def test_example_environment_never_supplies_working_secrets_or_mutable_images():
    raw = (ROOT / "deploy" / "env" / "production.env.example").read_text()
    assert "latest" not in raw.lower()
    assert raw.count("@sha256:REPLACE") == 8
    for line in raw.splitlines():
        if re.match(r"(?:DATABASE_URL|AUTH_DATABASE_URL|AMQP_URL|S3_ACCESS_KEY|S3_SECRET_KEY)=", line):
            assert "REPLACE" in line
