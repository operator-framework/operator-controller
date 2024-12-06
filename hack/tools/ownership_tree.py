#!/usr/bin/env python3
import json
import subprocess
import sys
import argparse
from collections import defaultdict

parser = argparse.ArgumentParser(description="Print a tree of ownership for all resources in a namespace, including cluster-scoped ones that reference the namespace.")
parser.add_argument("namespace", help="The namespace to inspect")
parser.add_argument("--no-events", action="store_true", help="Do not show Events kind grouping")
args = parser.parse_args()

NAMESPACE = args.namespace
SHOW_EVENTS = not args.no_events

def parse_api_resources_line(line):
    parts = [p for p in line.split(' ') if p]
    if len(parts) < 3:
        return None
    # KIND is last
    kind = parts[-1]
    # NAMESPACED is second-last
    namespaced_str = parts[-2].lower()
    namespaced = (namespaced_str == "true")
    # NAME is first
    name = parts[0]
    # Middle columns could be SHORTNAMES and APIVERSION
    # If len(middle) == 1: NAME APIVERSION NAMESPACED KIND
    # If len(middle) == 2: NAME SHORTNAMES APIVERSION NAMESPACED KIND
    middle = parts[1:-2]
    # We don't need these explicitly for logic now, just handle parsing consistently
    # APIVERSION unused directly, just ensuring correct parse.
    return name, "", "", namespaced, kind

# Gather resource info
kind_to_plural = {}
resource_info = []

try:
    all_resources_output = subprocess.check_output(["kubectl", "api-resources"], text=True).strip()
    lines = all_resources_output.split('\n')

    for line in lines[1:]:
        if not line.strip():
            continue
        parsed = parse_api_resources_line(line)
        if not parsed:
            continue
        name, _, _, is_namespaced, kind = parsed
        if kind not in kind_to_plural:
            kind_to_plural[kind] = name
        resource_info.append((kind, name, is_namespaced))

except subprocess.CalledProcessError:
    pass

uid_to_resource = {}
all_uids = set()

def get_resources_for_type(resource_name, namespaced):
    if namespaced:
        cmd = ["kubectl", "get", resource_name, "-n", NAMESPACE, "-o", "json", "--ignore-not-found"]
    else:
        cmd = ["kubectl", "get", resource_name, "-o", "json", "--ignore-not-found"]

    try:
        items_json = subprocess.check_output(cmd, text=True, stderr=subprocess.DEVNULL)
    except subprocess.CalledProcessError:
        return []
    if not items_json.strip():
        return []

    data = json.loads(items_json)
    if "items" not in data:
        return []

    items = data["items"]

    if namespaced:
        return items

    # Cluster-scoped: filter by namespace reference
    filtered = []
    for item in items:
        meta_ns = item.get("metadata", {}).get("namespace")
        spec_ns = item.get("spec", {}).get("namespace")
        if meta_ns == NAMESPACE or spec_ns == NAMESPACE:
            filtered.append(item)

    if filtered:
        return filtered

    # Fallback: try get by name if no filtered items
    try:
        single_json = subprocess.check_output(
            ["kubectl", "get", resource_name, NAMESPACE, "-o", "json", "--ignore-not-found"],
            text=True, stderr=subprocess.DEVNULL
        )
        if single_json.strip():
            single_data = json.loads(single_json)
            if "kind" in single_data and "metadata" in single_data:
                meta_ns = single_data.get("metadata", {}).get("namespace")
                spec_ns = single_data.get("spec", {}).get("namespace")
                if meta_ns == NAMESPACE or spec_ns == NAMESPACE:
                    filtered.append(single_data)
    except subprocess.CalledProcessError:
        pass

    return filtered

# Collect resources
for (kind, plural_name, is_namespaced) in resource_info:
    items = get_resources_for_type(plural_name, is_namespaced)
    for item in items:
        uid = item["metadata"]["uid"]
        k = item["kind"]
        nm = item["metadata"]["name"]
        owners = [(o["kind"], o["name"], o["uid"]) for o in item["metadata"].get("ownerReferences", [])]

        if k == "Event" and not SHOW_EVENTS:
            continue

        uid_to_resource[uid] = {
            "kind": k,
            "name": nm,
            "namespace": NAMESPACE,
            "uid": uid,
            "owners": owners
        }
        all_uids.add(uid)

owner_to_children = defaultdict(list)
for uid, res in uid_to_resource.items():
    for (o_kind, o_name, o_uid) in res["owners"]:
        owner_to_children[o_uid].append(uid)

# Identify top-level
top_level = []
for uid, res in uid_to_resource.items():
    if len(res["owners"]) == 0:
        top_level.append(uid)
    else:
        all_known = True
        for (_, _, o_uid) in res["owners"]:
            if o_uid not in uid_to_resource:
                all_known = False
                break
        if not all_known:
            top_level.append(uid)

kind_groups = defaultdict(list)
for uid in top_level:
    r = uid_to_resource[uid]
    if r["kind"] == "Event" and not SHOW_EVENTS:
        continue
    kind_groups[r["kind"]].append(uid)

pseudo_nodes = {}
for kind, uids in kind_groups.items():
    if kind == "Event" and not SHOW_EVENTS:
        continue

    plural = kind_to_plural.get(kind, kind.lower() + "s")
    pseudo_uid = f"PSEUDO_{kind.upper()}_NODE"
    pseudo_nodes[kind] = pseudo_uid
    uid_to_resource[pseudo_uid] = {
        "kind": plural.capitalize(),
        "name": "",
        "namespace": NAMESPACE,
        "uid": pseudo_uid,
        "owners": []
    }

    for child_uid in uids:
        owner_to_children[pseudo_uid].append(child_uid)

top_level_kinds = list(pseudo_nodes.values())

def pseudo_sort_key(uid):
    r = uid_to_resource[uid]
    return (r["kind"].lower(), r["name"].lower())

top_level_kinds.sort(key=pseudo_sort_key)

def resource_sort_key(uid):
    r = uid_to_resource[uid]
    return (r["kind"].lower(), r["name"].lower())

def print_tree(uid, prefix="", is_last=True):
    r = uid_to_resource[uid]
    branch = "└── " if is_last else "├── "
    if r['name']:
        print(prefix + branch + f"{r['kind']}/{r['name']}")
    else:
        print(prefix + branch + f"{r['kind']}")
    children = owner_to_children.get(uid, [])
    children.sort(key=resource_sort_key)
    child_prefix = prefix + ("    " if is_last else "│   ")
    for i, c_uid in enumerate(children):
        print_tree(c_uid, prefix=child_prefix, is_last=(i == len(children)-1))

for i, uid in enumerate(top_level_kinds):
    print_tree(uid, prefix="", is_last=(i == len(top_level_kinds)-1))