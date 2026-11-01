#!/usr/bin/env python3
"""Generate checked architecture regions without Docker or network access.

Compose is the wiring/build authority; package pointers supply teaching names,
manifest supplies node identity/endpoints, nginx supplies browser routes. Only
non-declarative semantics live in the reviewed overlay. Run from any directory.
"""

import argparse
from collections import defaultdict
import hashlib
import html
import json
from pathlib import Path
import re
import sys

import yaml

ROOT = Path(__file__).resolve().parents[1]
REGIONS = {
    "README.md": ("overview",),
    "docs/architecture-diagram.md": (
        "compose-us", "compose-eu", "network-us", "wiring-us",
        "network-eu", "wiring-eu", "request-flow",
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


def overlay_edges(overlay, package, view, resolve, compact=False):
    lines = []
    for item in overlay["edges"]:
        if view not in item["views"] or package.short not in item["packages"]:
            continue
        lines.append(f"    %% source: {item['source']}")
        text = item.get("compact_label", item["label"]) if compact else item["label"]
        lines.append(edge(resolve(item["from"]), resolve(item["to"]), text, "-.->"))
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


def chart(direction="LR"):
    return ['%%{init: {"flowchart": {"nodeSpacing": 22, "rankSpacing": 45, "padding": 10}}}%%',
            f"flowchart {direction}"]


def styles(lines, overlay, groups):
    for key, group in groups.items():
        colors = overlay["zone_styles"][key]
        lines.append(f"    style {group} fill:{colors['fill']},stroke:{colors['stroke']},color:#172033")


def service_label(package, key):
    sidecars = sorted(k for k, v in package.services.items()
                      if v.get("network_mode") == f"service:{key}")
    return key + ("\n" + ", ".join(sidecars) + " shares namespace" if sidecars else "")


def primary_network(package, key):
    networks = package.services[key].get("networks", {})
    node = package.teaching_nodes.get(package.nodes.get(key, {}).get("node"), {})
    for network in node.get("networks", []):
        if network in networks and network in package.zones:
            return network
    for network in networks:
        if network in package.zones:
            return network
    return "mgmt_net" if "mgmt_net" in networks else next(iter(networks))


def zone_name(package, key):
    return package.zones.get(key, {}).get(
        "name", "Management" if key == "mgmt_net" else key.removesuffix("_net").title())


def zone_graph(root, package, overlay, next_major, *, overview=False):
    lines = chart("TB")
    prefix = ("overview",) if overview else (package.short, "network")
    node = lambda key: ident(*prefix, key)
    groups = {}
    assignments = defaultdict(list)
    for key, service in sorted(package.services.items()):
        if key == "firewall" or service.get("network_mode"):
            continue
        network = ("platform" if overview and key in package.platform["source"]["services"]
                   else primary_network(package, key))
        assignments[network].append(key)
    firewall_nics = package.services["firewall"]["networks"]
    for key, members in assignments.items():
        group = groups[key] = ident(*prefix, "zone", key)
        if key == "platform":
            subnet = package.networks["mgmt_net"]["ipam"]["config"][0]["subnet"]
            title = f"Platform · management\n{subnet} · {firewall_nics['mgmt_net']['interface_name']}"
        else:
            subnet = package.networks[key]["ipam"]["config"][0]["subnet"]
            name = zone_name(package, key)
            iface = firewall_nics.get(key, {}).get("interface_name")
            title = f"{name} ·\n{subnet}" + (f" · {iface}" if iface else "")
        lines.append(f'    subgraph {group}["{label(title)}"]')
        lines.append("    direction TB")
        for service in members:
            text = service if overview else service_label(package, service)
            if service == "frontend":
                text += f" · Next.js {next_major}"
            lines.append(box(node(service), text))
        if key not in ("platform", "mgmt_net"):
            for first, second in zip(members, members[1:]):
                lines.append(f"    {node(first)} ~~~ {node(second)}")
        lines.append("    end")
    lines += [box(node("firewall"), "containd · firewall"),
              box(node("dockerd"), "Docker daemon"), box(node("browser"), "Browser")]
    lines.append(edge(node("browser"), node("proxy"), "portal · loopback 8088"))
    for network, nic in firewall_nics.items():
        group = groups["platform"] if overview and network == "mgmt_net" else groups[network]
        # Undirected attachments follow the reading order, avoiding a feedback
        # arc around the entire overview for the management connection.
        first, second = (group, node("firewall")) if network == "mgmt_net" else (node("firewall"), group)
        lines.append(edge(first, second, nic["interface_name"], "---"))
    # Show portal dispatch at a glance, not its complete route inventory.
    for (kind, destination), _ in routes(root, package).items():
        if kind != "proxy":
            continue
        service = destination.split(":")[0]
        if service == "firewall":
            continue  # Management hub attachment is already shown above.
        shared = set(package.services["proxy"]["networks"]) & set(package.services[service]["networks"])
        if len(shared) != 1:
            raise ValueError(f"proxy upstream must share exactly one network: {service}")
        network = shared.pop()
        delivery = package.manifest_networks.get(network, {}).get("delivery")
        if delivery:
            subnet = package.networks[network]["ipam"]["config"][0]["subnet"]
            lines.append(edge(node("proxy"), node(service), subnet, "-.->"))
        elif service in ("backend", "frontend"):
            lines.append(edge(node("proxy"), node(service), "API" if service == "backend" else "UI"))
    def resolve(key):
        key = key.split("@", 1)[0]
        return groups[key] if key in groups else node(key)
    lines += overlay_edges(overlay, package, "overview", resolve, compact=True)
    styles(lines, overlay, {("mgmt_net" if k == "platform" else k): v for k, v in groups.items()})
    if not overview:
        for column in overlay["zone_columns"].get(package.short, []):
            for first, second in zip(column, column[1:]):
                lines.append(f"    {groups[first]} ~~~ {groups[second]}")
    return fenced(lines)


def shared_network_notes(package):
    """Summarize secondary networks by primary zone, never by device edge."""
    lines = ["**Shared-network notes** (secondary NICs are listed in the table):", ""]
    for key, network in sorted(package.networks.items()):
        if key != "mgmt_net" and (key in package.platform["source"]["networks"]
                                 or key in package.zones or key in package.services["firewall"]["networks"]
                                 or package.manifest_networks.get(key, {}).get("delivery")):
            continue
        zones = set()
        for service, spec in package.services.items():
            if service != "firewall" and key in spec.get("networks", {}):
                primary = primary_network(package, service)
                zones.add(zone_name(package, primary))
        subnet = network["ipam"]["config"][0]["subnet"]
        purpose = "out-of-band management" if key == "mgmt_net" else "no firewall attachment"
        lines.append(f"- `{key}` · `{subnet}`: {purpose}; spans {', '.join(sorted(zones))}.")
    return "\n".join(lines)


def markdown_cell(value):
    return html.escape(str(value), quote=False).replace("|", "&#124;").replace("\n", "<br/>")


def wiring_table(package):
    networks = sorted(package.networks)
    row = lambda cells: "| " + " | ".join(markdown_cell(c) for c in cells) + " |"
    lines = [
        "One row per service. Namespace-sharing services have no independent NIC; "
        "— means no independent attachment. Cells show IP · interface where pinned.",
        "",
        row(["Service", *networks]),
        row(["---"] * (len(networks) + 1)),
    ]
    for key, spec in sorted(package.services.items()):
        name = key
        if spec.get("network_mode"):
            name += " (shares " + spec["network_mode"].removeprefix("service:") + " namespace)"
        cells = []
        for network in networks:
            nic = spec.get("networks", {}).get(network)
            cells.append(nic["ipv4_address"] + (" · " + nic["interface_name"] if nic.get("interface_name") else "")
                         if nic else "—")
        lines.append(row([name, *cells]))
    return "\n".join(lines)


def route_summary(paths):
    # Only public prefix locations are printed. Regex, exact matches and FUXA's
    # root API exceptions contribute to the count, never huge diagram labels.
    prefixes = [p for p in paths if p.startswith("/") and p.endswith("/")]
    prefixes.sort(key=lambda p: (not p.startswith("/apps/"), p.count("/"), len(p), p))
    shown = prefixes[:3]
    hidden = len(paths) - len(shown)
    return "\n".join(shown + ([f"+{hidden} more"] if hidden else []))


def request_graph(root, packages, overlay):
    lines = chart()
    node = lambda key: ident("request", key)
    upstreams = defaultdict(set)
    owners = defaultdict(set)
    ports = defaultdict(set)
    for package in packages:
        for (kind, destination), paths in routes(root, package).items():
            if kind == "proxy":
                upstreams[destination].update(paths)
                owners[destination].add(package.short.upper())
        for key, spec in package.services.items():
            ports[key].update(spec.get("ports", []))
    lines += [box(node("browser"), "Browser"), box(node("proxy"), "nginx proxy"),
              box(node("dockerd"), "Docker daemon"), box(node("debug"), "Host debug client")]
    counts = ", ".join(f"{p.short.upper()} {sum(bool(s.get('node')) for s in p.nodes.values())}"
                       for p in sorted(packages, key=lambda p: p.short, reverse=True))
    lines.append(box(node("terminals"), "WebSocket exec →\nmanifest topology nodes\n(" + counts + ")"))
    lines.append(edge(node("browser"), node("proxy"), "\n".join(sorted(ports["proxy"]))))
    lines.append(edge(node("debug"), node("firewall"), "\n".join(sorted(ports["firewall"]))))
    for destination, paths in sorted(upstreams.items()):
        service = destination.split(":")[0]
        suffix = " (" + "/".join(sorted(owners[destination])) + ")" if len(owners[destination]) == 1 else ""
        lines.append(box(node(service), destination + suffix))
        lines.append(edge(node("proxy"), node(service), route_summary(paths)))
    # Shared SDK/JWT/exec semantics appear once, not once per package. Retain
    # package-only logical edges (the US RTAC physics push) with scoped labels.
    seen = set()
    for package in packages:
        for item in overlay["edges"]:
            if "request" not in item["views"] or package.short not in item["packages"]:
                continue
            source, target = (item[k].split("@", 1)[0] for k in ("from", "to"))
            signature = (source, target, item["source"])
            if signature in seen:
                continue
            seen.add(signature)
            for service in (source, target):
                if service not in {"backend", "firewall", "dockerd", "terminals"}:
                    lines.append(box(node(service), service + " (" + "/".join(p.upper() for p in item["packages"]) + ")"))
            lines.append(f"    %% source: {item['source']}")
            lines.append(edge(node(source), node(target), item.get("compact_label", item["label"]), "-.->"))
    return fenced(lines)


def render(root):
    packages, overlay = load(root)
    by_short = {p.short: p for p in packages}
    next_version = json.loads((root / "frontend/package.json").read_text(encoding="utf-8"))["dependencies"]["next"]
    inventory = json.loads((root / ".github/release-images.json").read_text(encoding="utf-8"))["images"]
    next_major = next_version.split(".")[0]
    overview = zone_graph(root, by_short["us"], overlay, next_major, overview=True)
    result = {"overview": overview}
    for package in packages:
        result[f"compose-{package.short}"] = compose_graph(package, overlay)
        result[f"network-{package.short}"] = (zone_graph(root, package, overlay, next_major)
                                            + "\n\n" + shared_network_notes(package))
        result[f"wiring-{package.short}"] = wiring_table(package)
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
