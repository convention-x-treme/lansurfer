"""Assert safety invariants on normalized `docker compose config --format json` output."""

import json
import re
import sys
from urllib.parse import urlparse


def require(condition, message):
    if not condition:
        sys.exit(f"production Compose verification failed: {message}")


compose = json.load(sys.stdin)
services = compose["services"]
require(set(services) == {"lansurfer", "postgres"}, "unexpected required services")
app = services["lansurfer"]
db = services["postgres"]

require(not db.get("ports"), "PostgreSQL must not publish any host ports")
require("build" not in app, "production must use a release artifact, not a source build")
require(
    re.fullmatch(
        r"ghcr\.io/convention-x-treme/lansurfer:v[0-9]+\.[0-9]+\.[0-9]+@sha256:[0-9a-f]{64}",
        app["image"],
    ),
    "LANsurfer image must pin a version tag AND a digest",
)
require(app.get("read_only") is True, "app root filesystem must be read-only")
require("ALL" in app.get("cap_drop", []), "app capabilities must be dropped")
require(
    app.get("depends_on", {}).get("postgres", {}).get("condition") == "service_healthy",
    "app must wait for PostgreSQL readiness",
)
ports = app.get("ports", [])
require(len(ports) == 1, "exactly one LANsurfer port is expected")
require(ports[0].get("host_ip") == "127.0.0.1", "LANsurfer must bind only to host loopback")
require(str(ports[0].get("target")) == "8080", "unexpected app target port")

app_env = app["environment"]
db_env = db["environment"]
base_url = urlparse(app_env["LANSURFER_BASE_URL"])
require(base_url.scheme == "https" and base_url.netloc, "public URL must be HTTPS")
dsn = urlparse(app_env["LANSURFER_DATABASE_URL"])
require(
    dsn.hostname == "postgres" and dsn.port == 5432 and dsn.username == "lansurfer",
    "app database URL must target internal PostgreSQL",
)
require(dsn.password == db_env["POSTGRES_PASSWORD"], "database credentials do not match")


def has_volume(service, target):
    return any(item.get("type") == "volume" and item.get("target") == target for item in service.get("volumes", []))


require(has_volume(db, "/var/lib/postgresql"), "PostgreSQL data must be persistent")
require(has_volume(app, "/var/cache/lansurfer/covers"), "cover cache must be persistent")
print("Production Compose safety invariants verified.")
