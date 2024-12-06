#!/usr/bin/env python3
import json
import subprocess
import sys
import argparse
from collections import defaultdict

parser = argparse.ArgumentParser(description="Print a tree of ownership for all resources in a namespace, grouped by kind.")
parser.add_argument("namespace", help="The namespace to inspect")
parser.add_argument("--no-events", action="store_true", help="Do not show Events kind grouping")
args = parser.parse_args()

NAMESPACE = args.namespace
SHOW_EVENTS = not args.no_events

# Build a mapping of Kind -> plural name from `kubectl api-resources` table output
kind_to_plural = {}
try:
    all_resources_output = subprocess.check_output(["kubectl", "api-resources"], text=True).strip()
    lines = all_resources_output.split('\n')
    for line in lines[1:]:
        parts = [p for p in line.split(' ') if p]
        # NAME is first column, KIND is last column
        if len(parts) < 2:
            continue
        plural_name = parts[0]
        kind = parts[-1]
        if kind not in kind_to_plural:
            kind_to_plural[kind] = plural_name
except subprocess.CalledProcessError:
    # If this fails, we just won't have any plural mapping
    pass

# Get all namespaced resource types
api_resources_cmd = ["kubectl", "api-resources", "--verbs=list", "--namespaced", "-o", "name"]
resource_types = subprocess.check_output(api_resources_cmd, text=True).strip().split('\n')

uid_to_resource = {}
all_uids = set()

def get_resources_for_type(r_type):
    try:
        items_json = subprocess.check_output(
            ["kubectl", "get", r_type, "-n", NAMESPACE, "-o", "json"],
            text=True
        )
    except subprocess.CalledProcessError:
        return []
    data = json.loads(items_json)
    if "items" not in data:
        return []
    return data["items"]

# Collect all resources into uid_to_resource
for r_type in resource_types:
    items = get_resources_for_type(r_type)
    for item in items:
        uid = item["metadata"]["uid"]
        kind = item["kind"]
        name = item["metadata"]["name"]
        namespace = item["metadata"].get("namespace", NAMESPACE)
        owners = [(o["kind"], o["name"], o["uid"]) for o in item["metadata"].get("ownerReferences", [])]

        if kind == "Event" and not SHOW_EVENTS:
            continue

        uid_to_resource[uid] = {
            "kind": kind,
            "name": name,
            "namespace": namespace,
            "uid": uid,
            "owners": owners
        }
        all_uids.add(uid)

# Build a map of owner_uid -> [child_uids]
owner_to_children = defaultdict(list)
for uid, res in uid_to_resource.items():
    for (o_kind, o_name, o_uid) in res["owners"]:
        owner_to_children[o_uid].append(uid)

# Find top-level resources
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

# Group top-level resources by kind
kind_groups = defaultdict(list)
for uid in top_level:
    r = uid_to_resource[uid]
    if r["kind"] == "Event" and not SHOW_EVENTS:
        continue
    kind_groups[r["kind"]].append(uid)

# Create pseudo-nodes for each kind group
pseudo_nodes = {}
for kind, uids in kind_groups.items():
    # Use cluster known plural if available, else fallback
    plural = kind_to_plural.get(kind, kind.lower() + "s")
    # Capitalize the plural to make it look nice as a "kind" name
    # e.g. "configmaps" -> "Configmaps", "events" -> "Events"
    pseudo_uid = f"PSEUDO_{kind.upper()}_NODE"
    pseudo_nodes[kind] = pseudo_uid
    uid_to_resource[pseudo_uid] = {
        "kind": plural.capitalize(),
        "name": f"(all {plural})",
        "namespace": NAMESPACE,
        "uid": pseudo_uid,
        "owners": []
    }

    for child_uid in uids:
        owner_to_children[pseudo_uid].append(child_uid)

# Now our actual top-level nodes are these pseudo-nodes (one per kind)
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
    print(prefix + branch + f"{r['kind']}/{r['name']}")
    children = owner_to_children.get(uid, [])
    children.sort(key=resource_sort_key)
    child_prefix = prefix + ("    " if is_last else "│   ")
    for i, c_uid in enumerate(children):
        print_tree(c_uid, prefix=child_prefix, is_last=(i == len(children)-1))

# Print all top-level kind groupings
for i, uid in enumerate(top_level_kinds):
    print_tree(uid, prefix="", is_last=(i == len(top_level_kinds)-1))
    print()