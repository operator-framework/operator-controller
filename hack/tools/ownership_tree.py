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

        # If --no-events and resource is an Event, skip adding it altogether
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
        # May or may not exist in uid_to_resource
        owner_to_children[o_uid].append(uid)

# Find top-level resources
top_level = []
for uid, res in uid_to_resource.items():
    if len(res["owners"]) == 0:
        top_level.append(uid)
    else:
        # Check if all owners are known
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
        # Skip events if no-events is true
        continue
    kind_groups[r["kind"]].append(uid)

# We will create a pseudo-node for each kind that has top-level resources
# Named: KIND/(all <kind>s)
# Then list all those top-level resources under it.
#
# For example:
#  Deployment/(all deployments)
#    ├── Deployment/foo
#    └── Deployment/bar
#
# If there is only one resource of a given kind, we still group it under that kind node for consistency.

# We'll store these pseudo-nodes in uid_to_resource as well
pseudo_nodes = {}
for kind, uids in kind_groups.items():
    # Skip Events if SHOW_EVENTS is false
    if kind == "Event" and not SHOW_EVENTS:
        continue

    # Create a pseudo UID for the kind group node
    pseudo_uid = f"PSEUDO_{kind.upper()}_NODE"
    pseudo_nodes[kind] = pseudo_uid
    uid_to_resource[pseudo_uid] = {
        "kind": kind,
        "name": f"(all {kind.lower()}s)",
        "namespace": NAMESPACE,
        "uid": pseudo_uid,
        "owners": []  # top-level grouping node has no owners
    }

    # The top-level resources of this kind become children of this pseudo-node
    for child_uid in uids:
        owner_to_children[pseudo_uid].append(child_uid)

# Now our actual top-level nodes are these pseudo-nodes (one per kind)
top_level_kinds = list(pseudo_nodes.values())

# Sort these top-level kind nodes by their kind (and name) for stable output
def pseudo_sort_key(uid):
    r = uid_to_resource[uid]
    # The kind of this pseudo node is in r["kind"], and name is something like (all configmaps)
    # Sorting by kind and name is sufficient.
    return (r["kind"].lower(), r["name"].lower())

top_level_kinds.sort(key=pseudo_sort_key)

# For printing the tree
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