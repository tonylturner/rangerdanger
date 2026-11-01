#!/usr/bin/env python3
"""Generate checked Mermaid regions without Docker, network access or rendering.

Compose is the wiring/build authority; package pointers supply teaching names,
manifest supplies node identity/endpoints, nginx supplies browser routes. Only
non-declarative semantics live in the reviewed overlay. Run from any directory.
"""

import argparse
from collections import defaultdict
import hashlib
import json
from pathlib import Path
import re
import sys

import yaml

ROOT = Path(__file__).resolve().parents[1]
REGIONS = {
    "README.md": ("overview",),
    "docs/architecture-diagram.md": (
        "compose-us", "compose-eu", "network-us", "network-eu", "request-flow",
    ),
}
MARKER = re.compile(r"<!-- (BEGIN|END) GENERATED: ([\w-]+) -->")


def label(value):
    """Mermaid numeric entities protect HTML, quotes and grammar delimiters."""
    return "".join(f"#{ord(c)};" if c in '&<>"\'|`{}[]\\#' else c
                   for c in str(value)).replace("\n", "<br/>")


def ident(*parts):
    # Readable IDs plus a digest distinguish punctuation and separator collisions.
    value = json.dumps(parts, ensure_ascii=True)
    readable = re.sub(r"[^a-zA-Z0-9_]", "_", "_".join(map(str, parts)))
    return "n_" + readable + "_" + hashlib.sha256(value.encode()).hexdigest()[:10]


def box(node, text):
    return f'    {node}["{label(text)}"]'


def edge(a, b, text, arrow="-->"):
    return f'    {a} {arrow}|"{label(text)}"| {b}'


def fenced(lines):
    return "```mermaid\n" + "\n".join(lines) + "\n```"


def read_yaml(path):
    return yaml.safe_load(path.read_text(encoding="utf-8"))


def dependencies(service):
    deps = service.get("depends_on", {})
    if isinstance(deps, list):
        return {key: "service_started" for key in deps}
    result = {key: value["condition"] for key, value in deps.items()}
    if set(result.values()) - {"service_started", "service_healthy", "service_completed_successfully"}:
        raise ValueError("unsupported depends_on condition")
    return result


def nginx_directives(text):
    """Parse balanced nginx blocks, preserving quoted strings and regex tokens.

    This is a constrained syntax reader, not an nginx execution simulator. Any
    unknown proxy variable/upstream is rejected by routes(), never guessed.
    """
    pattern = r'''\s+|\#[^\n]*|"(?:\\.|[^"\\])*"|'(?:\\.|[^'\\])*'|[{};]|[^\s{};"']+'''
    tokens, offset = [], 0
    for match in re.finditer(pattern, text):
        if match.start() != offset:
            raise ValueError("unsupported nginx token")
        token = match.group()
        offset = match.end()
        if token.isspace() or token.startswith("#"):
            continue
        quoted = token[0] in "\"'"
        tokens.append((token[1:-1] if quoted else token, quoted))
    if offset != len(text):
        raise ValueError("unterminated nginx token")
    result, current = [], []
    stack = [result]
    for token, quoted in tokens:
        if token == ";" and not quoted:
            if not current:
                raise ValueError("empty nginx directive")
            stack[-1].append((current, None))
            current = []
        elif token == "{" and not quoted:
            if not current:
                raise ValueError("unnamed nginx block")
            children = []
            stack[-1].append((current, children))
            stack.append(children)
            current = []
        elif token == "}" and not quoted:
            if current or len(stack) == 1:
                raise ValueError("unbalanced nginx block")
            stack.pop()
        else:
            current.append(token)
    if current or len(stack) != 1:
        raise ValueError("unterminated nginx directive/block")
    return result


def routes(root, package):
    platform = nginx_directives((root / "proxy/nginx.conf").read_text(encoding="utf-8"))
    http = next(children for args, children in platform if args == ["http"])
    upstreams = {}
    for args, children in http:
        if args[0] == "upstream":
            servers = [a[1] for a, _ in children if a[0] == "server"]
            if len(servers) != 1:
                raise ValueError("architecture expects one server per upstream")
            upstreams[args[1]] = servers[0]
    server = next(children for args, children in http if args == ["server"])
    expanded = []
    for args, children in server:
        if args[0] == "include":
            if args[1:] != ["/etc/nginx/rd-routes/*.conf"]:
                raise ValueError("unsupported server include")
            expanded.extend(nginx_directives((package.directory / "nginx.routes.conf").read_text(encoding="utf-8")))
        else:
            expanded.append((args, children))
    groups = defaultdict(list)
    for args, children in expanded:
        if args[0] != "location":
            continue
        values = {a[0]: a[1:] for a, _ in children if a[0] in ("set", "proxy_pass", "return")}
        route = " ".join(args[1:])
        if "proxy_pass" in values:
            url = values["proxy_pass"][0]
            if url.startswith("$"):
                variable, url_value = values.get("set", ("", ""))
                if variable != url:
                    raise ValueError(f"unresolved nginx variable {url}")
                url = url_value
            match = re.fullmatch(r"http://([\w.-]+)(?::(\d+))?/?", url)
            if not match:
                raise ValueError(f"unsupported proxy_pass {url}")
            host, port = match.groups()
            target = upstreams.get(host, f"{host}:{port}" if port else host)
            service = target.split(":")[0]
            if service not in package.services:
                raise ValueError(f"unknown nginx upstream {target}")
            groups[("proxy", target)].append(route)
        elif "return" in values:
            code, *target = values["return"]
            # Error bodies are prose, not destinations; retain redirect targets.
            destination = f"{code} " + (target[0] if code.startswith("3") else "response")
            groups[("return", destination.strip())].append(route)
        else:
            raise ValueError(f"unsupported location without proxy/return: {route}")
    return {key: sorted(value) for key, value in sorted(groups.items())}


class Package:
    def __init__(self, root, directory, platform):
        self.directory = directory
        self.config = read_yaml(directory / "package.yml")
        self.key = self.config["id"]
        self.short = self.key.split("-")[0]
        self.topology_path = (directory / self.config["topology"]).resolve()
        self.topology_path.relative_to(root.resolve())
        self.topology = read_yaml(self.topology_path)
        self.manifest = json.loads((directory / "manifest.json").read_text(encoding="utf-8"))
        self.source = read_yaml(directory / "compose.source.yml")
        self.release = read_yaml(directory / "compose.release.yml")
        self.services = {**platform["source"]["services"], **self.source["services"]}
        self.nodes = {s["key"]: s for s in self.manifest["services"]}
        self.manifest_networks = {n["key"]: n for n in self.manifest["networks"]}
        self.teaching_nodes = {n["id"]: n for n in self.topology["nodes"]}
        self.zones = {n["id"]: n for n in self.topology["networks"]}
        # Resolve external networks by their Docker name, not YAML key or order.
        self.networks = {**platform["source"]["networks"], **self.source["networks"]}
        owners = {v["name"]: v for v in platform["source"]["networks"].values()}
        for key, network in self.networks.items():
            if network.get("external"):
                if network["name"] not in owners:
                    raise ValueError(f"unknown external network {network['name']}")
                self.networks[key] = owners[network["name"]]
        self.platform = platform
        if set(self.nodes) != set(self.source["services"]):
            raise ValueError(f"manifest/Compose service set differs: {self.key}")
        # Never hide a mode-specific wiring change behind one shared graph.
        for source, release in ((platform["source"], platform["release"]), (self.source, self.release)):
            if source["networks"] != release["networks"] or set(source["services"]) != set(release["services"]):
                raise ValueError(f"source/release wiring differs: {self.key}")
            for key, service in source["services"].items():
                for field in ("networks", "network_mode", "ports", "depends_on"):
                    if service.get(field) != release["services"][key].get(field):
                        raise ValueError(f"source/release {key}.{field} differs")

    def title(self, key):
        node = self.nodes.get(key, {}).get("node")
        name = self.teaching_nodes.get(node, {}).get("name")
        return f"{key} · {name}" if name else key


def load(root):
    platform = {mode: read_yaml(root / filename) for mode, filename in (
        ("source", "docker-compose.yml"), ("release", "docker-compose.release.yml"))}
    packages = [Package(root, path.parent, platform) for path in
                sorted((root / "lab-definitions/packages").glob("*/package.yml"))]
    if {p.short for p in packages} != {"us", "eu"} or len(packages) != 2:
        raise ValueError("expected US and EU packages; declare new diagram regions for new packages")
    overlay = read_yaml(root / "docs/architecture/overlay.yml")
    if overlay["schema"] != 1:
        raise ValueError("unsupported overlay schema")
    for item in overlay["edges"]:
        path, citation = item["source"].rsplit(":", 1)
        if not citation or not (root / path).is_file():
            raise ValueError(f"overlay source does not exist: {item['source']}")
    return packages, overlay


def overlay_edges(overlay, package, view, resolve):
    lines = []
    for item in overlay["edges"]:
        if view not in item["views"] or package.short not in item["packages"]:
            continue
        lines.append(f"    %% source: {item['source']}")
        lines.append(edge(resolve(item["from"]), resolve(item["to"]), item["label"], "-.->"))
    return lines


def image_description(source, release):
    build = source.get("build")
    if build:
        if not isinstance(build, dict):
            raise ValueError("expected mapped build context")
        description = "source: build " + build["dockerfile"]
        if build.get("target"):
            description += " :" + build["target"]
        if build.get("context", ".") != ".":
            description += "\ncontext: " + build["context"]
        if source.get("image"):
            description += "\ntag: " + source["image"]
    else:
        description = "source: pull " + source["image"]
    return description + "\nrelease: pull " + release["image"]


def compose_graph(package, overlay):
    lines = ["flowchart TB"]
    ids = {}
    for scope, source, release in (
        ("platform", package.platform["source"], package.platform["release"]),
        ("range", package.source, package.release),
    ):
        group = ident(package.short, scope)
        ids[scope] = group
        lines.append(f'    subgraph {group}["{label(source["name"] + " · " + scope)}"]')
        for key, service in sorted(source["services"].items()):
            node = ids[key] = ident(package.short, "compose", key)
            lines.append(box(node, key + "\n" + image_description(service, release["services"][key])))
        lines.append("    end")
        for key, service in sorted(source["services"].items()):
            for target, condition in sorted(dependencies(service).items()):
                if target not in source["services"]:
                    raise ValueError(f"unknown dependency {target}")
                arrow = "-->" if condition == "service_started" else "-.->"
                lines.append(edge(ids[key], ids[target], condition, arrow))
            if service.get("network_mode"):
                mode, owner = service["network_mode"].split(":", 1)
                if mode != "service" or owner not in source["services"]:
                    raise ValueError("unsupported network namespace mode")
                lines.append(edge(ids[key], ids[owner], "shares network namespace", "---"))
    lines += overlay_edges(overlay, package, "compose", ids.__getitem__)
    return fenced(lines)


def network_graph(package, overlay, overview=False, next_major=None, root=None):
    lines = ["flowchart LR"]
    aliases = defaultdict(list)
    groups = {}
    for key, network in sorted(package.networks.items()):
        group = groups[key] = ident(package.short, "network", key)
        config = network["ipam"]["config"]
        zone = package.zones.get(key, {})
        title = f'{key} · {config[0]["subnet"]}'
        docker_name = (package.manifest_networks[key]["name"] if key in package.manifest_networks
                       else network["name"])
        title += f"\n{docker_name}"
        if zone:
            title += f' · {zone["name"]}'
        if network.get("internal"):
            title += " · internal"
        lines.append(f'    subgraph {group}["{label(title)}"]')
        for service_key, service in sorted(package.services.items()):
            attachment = service.get("networks", {}).get(key)
            if attachment is None:
                continue
            node = ident(package.short, "nic", key, service_key)
            aliases[service_key].append(node)
            text = package.title(service_key)
            if service_key == "frontend" and next_major:
                text += f" · Next.js {next_major}"
            text += "\n" + attachment["ipv4_address"]
            if attachment.get("interface_name"):
                text += " · " + attachment["interface_name"]
            lines.append(box(node, text))
            # Sidecars have no independent NIC or topology node.
            for sidecar, spec in sorted(package.services.items()):
                if spec.get("network_mode") == f"service:{service_key}":
                    shared = ident(package.short, "nic", key, sidecar)
                    aliases[sidecar].append(shared)
                    lines.append(box(shared, f"{sidecar}\nshares {service_key} namespace"))
                    lines.append(edge(shared, node, "namespace", "---"))
        lines.append("    end")
    # One identity can have multiple NIC boxes; dotted links are identity, not traffic.
    for service, nodes in sorted(aliases.items()):
        for target in nodes[1:]:
            lines.append(edge(nodes[0], target, f"same {service}", "-.-"))
    if overview:
        lines.append(box(ident("dockerd"), "Docker daemon · host"))
        lines.append(box(ident("browser"), "Student browser"))
        for port in package.services["proxy"]["ports"]:
            lines.append(edge(ident("browser"), ident(package.short, "nic", "mgmt_net", "proxy"), port))
        for (kind, destination), paths in routes(root, package).items():
            if kind != "proxy":
                continue
            service = destination.split(":")[0]
            shared = set(package.services["proxy"]["networks"]) & set(package.services[service]["networks"])
            if len(shared) != 1:
                raise ValueError(f"proxy upstream must share exactly one network: {service}")
            network = shared.pop()
            summary = ", ".join(paths[:2])
            if len(paths) > 2:
                summary += f" (+{len(paths) - 2} locations)"
            lines.append(edge(ident(package.short, "nic", network, "proxy"),
                              ident(package.short, "nic", network, service),
                              destination + "\n" + summary))
        def resolve(key):
            if key == "dockerd":
                return ident("dockerd")
            if key in groups:
                return groups[key]
            if "@" in key:
                service, network = key.split("@", 1)
                node = ident(package.short, "nic", network, service)
                if node not in aliases[service]:
                    raise ValueError(f"unknown overlay NIC {key}")
                return node
            if key == "firewall":
                return ident(package.short, "nic", "mgmt_net", key)
            return aliases[key][0]
        lines += overlay_edges(overlay, package, "overview", resolve)
    return fenced(lines)


def request_graph(root, packages, overlay):
    lines = ["flowchart LR", box(ident("browser"), "Student browser"),
             box(ident("host-client"), "Host debug client · direct firewall ports"),
             box(ident("dockerd"), "Docker daemon · host")]
    for package in packages:
        prefix = package.short
        node = lambda key: ident(prefix, "request", key)
        lines.append(f'    subgraph {ident(prefix, "requests")}["{label(package.config["title"])}"]')
        for key in ("backend", "frontend", "proxy"):
            lines.append(box(node(key), key))
        for key, service in sorted(package.nodes.items()):
            endpoints = "\n".join(f"{name}: {url}" for name, url in sorted(service.get("endpoints", {}).items()))
            lines.append(box(node(key), package.title(key) + ("\n" + endpoints if endpoints else "")))
        terminals = [f'{s["node"]} → {s["key"]}' for s in package.nodes.values() if s.get("node")]
        lines.append(box(node("terminals"), "Terminal targets (manifest nodes only)\n" + "\n".join(sorted(terminals))))
        for key, service in sorted(package.services.items()):
            for port in sorted(service.get("ports", [])):
                if not isinstance(port, str):
                    raise ValueError("unsupported long-form port mapping")
                port_id = node(f"port:{key}:{port}")
                lines.append(box(port_id, port))
                client = "browser" if key == "proxy" else "host-client"
                lines.append(edge(ident(client), port_id, "loopback published port"))
                lines.append(edge(port_id, node(key), "published to container"))
        for index, ((kind, destination), paths) in enumerate(routes(root, package).items()):
            route_id = node(f"route:{index}")
            title = "\n".join(paths)
            if destination.startswith("backend"):
                title += "\nexcept package-specific nginx locations"
            lines.append(box(route_id, title))
            lines.append(edge(node("proxy"), route_id, "nginx location"))
            if kind == "proxy":
                lines.append(edge(route_id, node(destination.split(":")[0]), destination))
            else:
                response_id = node(destination)
                lines.append(box(response_id, destination))
                lines.append(edge(route_id, response_id, "return"))
        def resolve(key):
            if key == "dockerd":
                return ident("dockerd")
            service = key.split("@", 1)[0]
            if service not in package.services and service != "terminals":
                raise ValueError(f"unknown overlay service {service}")
            return node(service)
        lines += overlay_edges(overlay, package, "request", resolve)
        lines.append("    end")
    return fenced(lines)


def render(root):
    packages, overlay = load(root)
    by_short = {p.short: p for p in packages}
    next_version = json.loads((root / "frontend/package.json").read_text(encoding="utf-8"))["dependencies"]["next"]
    inventory = json.loads((root / ".github/release-images.json").read_text(encoding="utf-8"))["images"]
    overview = network_graph(by_short["us"], overlay, overview=True, next_major=next_version.split(".")[0], root=root)
    result = {"overview": overview}
    for package in packages:
        result[f"compose-{package.short}"] = compose_graph(package, overlay)
        result[f"network-{package.short}"] = network_graph(package, overlay)
    # Keep inventory prose within a checked region too: never a second hardcoded count.
    targets = sum(i["dockerfile"] == "services/Dockerfile" for i in inventory)
    amd64 = sorted(i["image"] for i in inventory if i["platforms"] == ["linux/amd64"])
    result["compose-us"] += (
        f"\n\nRelease inventory: **{len(inventory)} first-party images**, including **{targets} shared-service targets**.\n"
        f"amd64-only: {', '.join(f'`{name}`' for name in amd64)}.\n"
        "Source build bases are not the same inventory as upstream service pulls."
    )
    result["request-flow"] = request_graph(root, packages, overlay)
    return result


def replace_regions(text, expected, rendered):
    matches = list(MARKER.finditer(text))
    seen, spans, opened = set(), [], None
    # A malformed marker must not disappear into unowned prose.
    if text.count("<!-- BEGIN GENERATED") + text.count("<!-- END GENERATED") != len(matches):
        raise ValueError("malformed generated marker")
    for match in matches:
        kind, name = match.groups()
        if name not in expected:
            raise ValueError(f"unexpected generated region {name}")
        if kind == "BEGIN":
            if opened or name in seen:
                raise ValueError(f"duplicate/nested generated region {name}")
            opened = (name, match.end())
            seen.add(name)
        else:
            if not opened or opened[0] != name:
                raise ValueError(f"unbalanced generated region {name}")
            spans.append((name, opened[1], match.start()))
            opened = None
    if opened or seen != set(expected):
        raise ValueError(f"missing/unbalanced generated regions: {', '.join(sorted(set(expected) - seen))}")
    changed = []
    for name, start, end in reversed(spans):
        replacement = "\n" + rendered[name] + "\n"
        if text[start:end] != replacement:
            changed.append(name)
        text = text[:start] + replacement + text[end:]
    return text, sorted(changed)


def generate(root, check=False):
    rendered = render(root)
    outputs, drift = {}, []
    # Validate every region before performing any write.
    for filename, names in REGIONS.items():
        try:
            outputs[filename], changed = replace_regions((root / filename).read_bytes().decode("utf-8"), names, rendered)
        except ValueError as exc:
            raise ValueError(f"{filename}: {exc}") from exc
        drift.extend(f"{filename}: {name}" for name in changed)
    if check:
        for region in drift:
            print(f"architecture drift: {region}", file=sys.stderr)
        return 1 if drift else 0
    for filename, text in outputs.items():
        if (root / filename).read_bytes().decode("utf-8") != text:
            (root / filename).write_bytes(text.encode("utf-8"))
    return 0


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--check", action="store_true", help="report drift without writing")
    args = parser.parse_args()
    try:
        return generate(ROOT, check=args.check)
    except (ValueError, KeyError, OSError, StopIteration, yaml.YAMLError) as exc:
        print(f"architecture generation failed: {exc}", file=sys.stderr)
        return 1


if __name__ == "__main__":
    sys.exit(main())
