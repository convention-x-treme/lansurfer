"""Validate the resolved production Compose model without printing secrets.

Usage: docker compose ... config --format json | python3 scripts/test-production-compose.py
Optional --test-mutations checks that dangerous configuration changes fail.
"""

import copy
import ipaddress
import json
import re
import sys
from urllib.parse import urlsplit


class UnsafeConfiguration(ValueError):
    pass


def require(condition, message):
    if not condition:
        raise UnsafeConfiguration(message)


def volume_at(service, target):
    return any(
        mount.get("type") == "volume" and mount.get("target") == target
        for mount in service.get("volumes", [])
    )


def validate(compose):
    services = compose.get("services", {})
    require(set(services) == {"lansurfer", "postgres"}, "only LANsurfer and PostgreSQL may run")
    app, db = services["lansurfer"], services["postgres"]

    require("build" not in app, "production must never build the application from source")
    require(
        re.fullmatch(
            r"ghcr\.io/convention-x-treme/lansurfer:v\d+\.\d+\.\d+@sha256:[a-f0-9]{64}",
            app.get("image", ""),
        ),
        "LANsurfer must be pinned by stable version and digest",
    )
    require(
        re.fullmatch(r"postgres:\d+\.\d+-alpine@sha256:[a-f0-9]{64}", db.get("image", "")),
        "PostgreSQL must be pinned by minor version and digest",
    )
    require(not db.get("ports"), "PostgreSQL must not publish ports to the host")
    require("build" not in db, "PostgreSQL must use an image")
    require("network_mode" not in db, "PostgreSQL must remain on a Compose network")
    require("network_mode" not in app, "LANsurfer must remain on a Compose network")
    require(app.get("privileged") is not True, "LANsurfer must not be privileged")
    require(app.get("read_only") is True, "LANsurfer root filesystem must be read-only")
    require("ALL" in app.get("cap_drop", []), "LANsurfer must drop Linux capabilities")
    require("no-new-privileges:true" in app.get("security_opt", []), "no-new-privileges is required")
    require(str(app.get("user", "non-root-image-user")) not in ("0", "root", "0:0"), "LANsurfer must not run as root")

    dependency = app.get("depends_on", {}).get("postgres", {})
    require(dependency.get("condition") == "service_healthy", "LANsurfer must wait for DB health")
    require(dependency.get("restart") is True, "LANsurfer must restart after an explicit DB restart")
    db_health = db.get("healthcheck", {}).get("test", [])
    health_command = " ".join(db_health) if isinstance(db_health, list) else ""
    require("psql -h 127.0.0.1" in health_command and "PGPASSWORD=" in health_command
            and "SELECT 1" in health_command,
            "PostgreSQL healthcheck must authenticate a TCP SQL query")

    ports = app.get("ports", [])
    require(len(ports) == 1, "LANsurfer must have exactly one published port")
    port = ports[0]
    require(port.get("host_ip") == "127.0.0.1", "LANsurfer must bind only to loopback")
    require(str(port.get("target")) == "8080", "unexpected application container port")
    require(port.get("protocol", "tcp") == "tcp", "only HTTP TCP may be published")
    require(str(port.get("published", "")).isdigit() and 1 <= int(port["published"]) <= 65535,
            "invalid host application port")

    require(volume_at(db, "/var/lib/postgresql"), "PostgreSQL 18 data needs a persistent named volume")
    require(volume_at(app, "/var/cache/lansurfer/covers"), "media cache needs a named volume")
    require(len(db.get("volumes", [])) == 1, "unexpected PostgreSQL mounts")
    require(len(app.get("volumes", [])) == 1, "unexpected LANsurfer mounts")

    app_env, db_env = app.get("environment", {}), db.get("environment", {})
    password = db_env.get("POSTGRES_PASSWORD", "")
    require(isinstance(password, str) and re.fullmatch(r"[a-fA-F0-9]{64}", password),
            "POSTGRES_PASSWORD must be 64 hexadecimal characters (openssl rand -hex 32)")
    require(app_env.get("LANSURFER_LISTEN_ADDR") == ":8080", "unexpected application listen address")
    base = urlsplit(app_env.get("LANSURFER_BASE_URL", ""))
    require(base.scheme == "https" and bool(base.hostname) and not base.username and not base.password
            and base.path in ("", "/") and not base.query and not base.fragment,
            "LANSURFER_BASE_URL must be a single public HTTPS origin without path or credentials")

    dsn = urlsplit(app_env.get("LANSURFER_DATABASE_URL", ""))
    require(dsn.scheme in ("postgres", "postgresql") and dsn.hostname == "postgres"
            and dsn.port == 5432 and dsn.username == "lansurfer"
            and dsn.path == "/lansurfer" and dsn.query == "sslmode=disable"
            and dsn.password == password,
            "LANsurfer must use the internal PostgreSQL URL and matching password")
    require(db_env.get("POSTGRES_DB") == "lansurfer" and db_env.get("POSTGRES_USER") == "lansurfer",
            "database name/user mismatch")
    trusted = app_env.get("LANSURFER_TRUSTED_PROXIES", "")
    for item in filter(None, (entry.strip() for entry in trusted.split(","))):
        try:
            prefix = ipaddress.ip_network(item, strict=False)
        except ValueError as error:
            raise UnsafeConfiguration("LANSURFER_TRUSTED_PROXIES requires IPs or CIDRs") from error
        require(prefix.prefixlen != 0, "LANSURFER_TRUSTED_PROXIES must not trust the entire internet")

    oidc_issuer = bool(app_env.get("LANSURFER_OIDC_ISSUER"))
    oidc_client = bool(app_env.get("LANSURFER_OIDC_CLIENT_ID"))
    require(oidc_issuer == oidc_client, "OIDC issuer and client ID must be configured together")
    discord_id = bool(app_env.get("LANSURFER_DISCORD_CLIENT_ID"))
    discord_secret = bool(app_env.get("LANSURFER_DISCORD_CLIENT_SECRET"))
    require(discord_id == discord_secret, "Discord login needs client ID and client secret")
    twitch_login = app_env.get("LANSURFER_TWITCH_AUTH_ENABLED", "false").lower() == "true"
    if twitch_login:
        require(bool(app_env.get("LANSURFER_TWITCH_CLIENT_ID")) and
                bool(app_env.get("LANSURFER_TWITCH_CLIENT_SECRET")),
                "Twitch login needs client ID and client secret")
    require(oidc_issuer or discord_id or twitch_login, "configure at least one login provider")
    if discord_id:
        require(bool(app_env.get("LANSURFER_LEGAL_ENTITY_NAME")) and
                bool(app_env.get("LANSURFER_LEGAL_CONTACT_EMAIL")),
                "Discord login requires operator legal entity name and contact email")


def test_mutations(valid):
    """Check fail-closed behavior against the real, resolved Compose model."""
    cases = {
        "public database": lambda x: x["services"]["postgres"].update(ports=[{"published": "5432"}]),
        "public application": lambda x: x["services"]["lansurfer"]["ports"][0].update(host_ip="0.0.0.0"),
        "mutable application": lambda x: x["services"]["lansurfer"].update(image="ghcr.io/convention-x-treme/lansurfer:latest"),
        "mutable database": lambda x: x["services"]["postgres"].update(image="postgres:18.6-alpine"),
        "weak password": lambda x: x["services"]["postgres"]["environment"].update(POSTGRES_PASSWORD="password"),
        "insecure origin": lambda x: x["services"]["lansurfer"]["environment"].update(LANSURFER_BASE_URL="http://lansurfer.example.org"),
        "missing database volume": lambda x: x["services"]["postgres"].update(volumes=[]),
        "missing restart handling": lambda x: x["services"]["lansurfer"]["depends_on"]["postgres"].update(restart=False),
        "no login provider": lambda x: x["services"]["lansurfer"]["environment"].update(LANSURFER_DISCORD_CLIENT_ID="", LANSURFER_DISCORD_CLIENT_SECRET=""),
        "excessive proxy trust": lambda x: x["services"]["lansurfer"]["environment"].update(LANSURFER_TRUSTED_PROXIES="0.0.0.0/0"),
    }
    for name, mutate in cases.items():
        candidate = copy.deepcopy(valid)
        mutate(candidate)
        try:
            validate(candidate)
        except UnsafeConfiguration:
            continue
        raise UnsafeConfiguration(f"negative test failed: {name} was accepted")


def main():
    if len(sys.argv) > 2 or (len(sys.argv) == 2 and sys.argv[1] != "--test-mutations"):
        sys.exit("usage: test-production-compose.py [--test-mutations]")
    try:
        compose = json.load(sys.stdin)
        validate(compose)
        if sys.argv[1:] == ["--test-mutations"]:
            test_mutations(compose)
    except (KeyError, TypeError, ValueError) as error:
        sys.exit(f"production Compose validation failed: {error}")
    print("Production Compose configuration and safety checks passed.")


if __name__ == "__main__":
    main()
