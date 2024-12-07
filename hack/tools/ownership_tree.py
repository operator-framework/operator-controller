#!/usr/bin/env python3.11
import json
import os
from openai import OpenAI

client = OpenAI(api_key=os.getenv("OPENAI_API_KEY"))
import subprocess
import sys
import argparse
from collections import defaultdict

parser = argparse.ArgumentParser(description="Print a tree of ownership for all resources in a namespace, optionally gather cluster extension state.")
parser.add_argument("namespace", help="The namespace to inspect")
parser.add_argument("--no-events", action="store_true", help="Do not show Events kind grouping")
parser.add_argument("--with-event-info", action="store_true", help="Show additional info (message) for Events")
parser.add_argument("--gather-cluster-extension-state", action="store_true",
                    help="Gather and save a compressed fingerprint of the cluster extension state to a file.")
parser.add_argument("--no-tree", action="store_true", help="Do not print the tree output (only used if gather-cluster-extension-state is set).")
parser.add_argument("--prompt", action="store_true", help="Create the fingerprint file (if needed) and send it to OpenAI for diagnosis.")
args = parser.parse_args()

NAMESPACE = args.namespace

# If --prompt is used, we also want full info and to gather fingerprint, regardless of other flags
if args.prompt:
    args.gather_cluster_extension_state = True

if args.gather_cluster_extension_state:
    SHOW_EVENTS = True
    WITH_EVENT_INFO = True
else:
    SHOW_EVENTS = not args.no_events
    WITH_EVENT_INFO = args.with_event_info

def parse_api_resources_line(line):
    parts = [p for p in line.split(' ') if p]
    if len(parts) < 3:
        return None
    kind = parts[-1]
    namespaced_str = parts[-2].lower()
    namespaced = (namespaced_str == "true")
    name = parts[0]
    return name, namespaced, kind

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
        name, is_namespaced, kind = parsed
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

    # cluster-scoped: filter by namespace reference
    filtered = []
    for item in items:
        meta_ns = item.get("metadata", {}).get("namespace")
        spec_ns = item.get("spec", {}).get("namespace")
        if meta_ns == NAMESPACE or spec_ns == NAMESPACE:
            filtered.append(item)

    if filtered:
        return filtered

    # fallback by name
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
    if kind == "Event" and not SHOW_EVENTS:
        continue
    items = get_resources_for_type(plural_name, is_namespaced)
    for item in items:
        uid = item["metadata"]["uid"]
        k = item["kind"]
        nm = item["metadata"]["name"]
        owners = [(o["kind"], o["name"], o["uid"]) for o in item["metadata"].get("ownerReferences", [])]

        if k == "Event" and not SHOW_EVENTS:
            continue

        res_entry = {
            "kind": k,
            "name": nm,
            "namespace": NAMESPACE,
            "uid": uid,
            "owners": owners
        }

        if k == "Event" and WITH_EVENT_INFO:
            res_entry["message"] = item.get("message", "")

        uid_to_resource[uid] = res_entry
        all_uids.add(uid)

from collections import defaultdict
owner_to_children = defaultdict(list)
for uid, res in uid_to_resource.items():
    for (_, _, o_uid) in res["owners"]:
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
for kind, uids_ in kind_groups.items():
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
    for child_uid in uids_:
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
    if WITH_EVENT_INFO and r['kind'] == "Event" and "message" in r:
        child_prefix = prefix + ("    " if is_last else "│   ")
        print(child_prefix + "└── message: " + r["message"])
    children = owner_to_children.get(uid, [])
    children.sort(key=resource_sort_key)
    child_prefix = prefix + ("    " if is_last else "│   ")
    for i, c_uid in enumerate(children):
        print_tree(c_uid, prefix=child_prefix, is_last=(i == len(children)-1))


def extract_resource_summary(kind, name, namespace):
    is_namespaced = (namespace is not None and namespace != "")
    cmd = ["kubectl", "get", kind.lower()+"/"+name]
    if is_namespaced:
        cmd.extend(["-n", namespace])
    cmd.extend(["-o", "json", "--ignore-not-found"])

    try:
        out = subprocess.check_output(cmd, text=True, stderr=subprocess.DEVNULL)
        if not out.strip():
            return {}
        data = json.loads(out)
    except subprocess.CalledProcessError:
        return {}

    summary = {
        "kind": data.get("kind", kind),
        "name": data.get("metadata", {}).get("name", name),
        "namespace": data.get("metadata", {}).get("namespace", namespace)
    }

    conditions = data.get("status", {}).get("conditions", [])
    if conditions:
        summary["conditions"] = [
            {
                "type": c.get("type"),
                "status": c.get("status"),
                "reason": c.get("reason"),
                "message": c.get("message")
            } for c in conditions
        ]

    # For pods/deployments, extract container images
    if data.get("kind") in ["Pod", "Deployment"]:
        images = []
        if data["kind"] == "Pod":
            containers = data.get("spec", {}).get("containers", [])
            for cont in containers:
                images.append({"name": cont.get("name"), "image": cont.get("image")})
        elif data["kind"] == "Deployment":
            containers = data.get("spec", {}).get("template", {}).get("spec", {}).get("containers", [])
            for cont in containers:
                images.append({"name": cont.get("name"), "image": cont.get("image")})
        if images:
            summary["containers"] = images

    # For Events, show reason and message
    if data.get("kind") == "Event":
        summary["reason"] = data.get("reason")
        summary["message"] = data.get("message")

    metadata = data.get("metadata", {})
    if metadata.get("labels"):
        summary["labels"] = metadata["labels"]
    if metadata.get("annotations"):
        summary["annotations"] = metadata["annotations"]

    return summary

def load_fingerprint(file_path):
    with open(file_path, 'r') as f:
        return json.load(f)

def generate_prompt(fingerprint):
    prompt = """
You are an expert in Kubernetes operations and diagnostics. I will provide you with a JSON file that represents a snapshot ("fingerprint") of the entire state of a Kubernetes namespace focusing on a particular ClusterExtension and all related resources. This fingerprint includes:

- The ClusterExtension itself.
- All resources in the namespace that are either owned by or possibly needed by the ClusterExtension.
- Key details such as resource conditions, event messages, container images (with references), and minimal metadata.

Your task is:
1. Analyze the provided fingerprint to determine if there are any issues with the ClusterExtension, its related resources, or its configuration.
2. If issues are found, provide a diagnosis of what might be wrong and suggest steps to fix them.
3. If no issues appear, acknowledge that the ClusterExtension and its resources seem healthy.
4. Keep your answer concise and action-focused, as the output will be used by a human operator to troubleshoot or confirm the health of their cluster.

**Important Details:**
- The fingerprint might contain events that show what happened in the cluster recently.
- Check conditions of deployments, pods, and other resources to see if they indicate errors or warnings.
- Look at event messages for hints about failures, restarts, or other anomalies.
- Consider if all necessary resources (like ServiceAccounts, ConfigMaps, or other dependencies) are present and seemingly functional.

**BEGIN FINGERPRINT**
{fingerprint}
**END FINGERPRINT**

Please provide a summarized diagnosis and suggested fixes below:
    """.format(fingerprint=json.dumps(fingerprint, indent=2))
    return prompt

def send_to_openai(prompt, model="gpt-4o"):
    try:
        if os.getenv("OPENAI_API_KEY") is None:
            raise ValueError("OPENAI_API_KEY environment variable is not set.")
        response = client.chat.completions.create(model=model,
        messages=[{"role": "user", "content": prompt}])
        message_content = response.choices[0].message.content
        return message_content
    except Exception as e:
        return f"Error communicating with OpenAI API: {e}"

def gather_fingerprint(namespace):
    ce_uids = [uid for uid, res in uid_to_resource.items() if res["kind"] == "ClusterExtension" and res["namespace"] == namespace]
    if not ce_uids:
        return []

    all_images = {}
    image_ref_count = 0

    def process_resource(uid):
        nonlocal image_ref_count
        r = uid_to_resource[uid]
        k = r["kind"]
        nm = r["name"]
        ns = r["namespace"]
        summary = extract_resource_summary(k, nm, ns)
        if "containers" in summary:
            new_containers = []
            for c in summary["containers"]:
                img = c["image"]
                if img not in all_images:
                    image_ref_count += 1
                    ref_name = f"image_ref_{image_ref_count}"
                    all_images[img] = ref_name
                c["imageRef"] = all_images[img]
                del c["image"]
                new_containers.append(c)
            summary["containers"] = new_containers
        return summary

    results = []
    for ce_uid in ce_uids:
        fingerprint = {}
        for uid in uid_to_resource:
            r = uid_to_resource[uid]
            key = f"{r['kind']}/{r['name']}"
            fp = process_resource(uid)
            fingerprint[key] = fp
        if all_images:
            fingerprint["_image_map"] = {v: k for k, v in all_images.items()}
        ce_name = uid_to_resource[ce_uid]["name"]
        fname = f"{ce_name}-state.json"
        with open(fname, "w") as f:
            json.dump(fingerprint, f, indent=2)
        results.append(fname)
    return results

state_files = []
if args.gather_cluster_extension_state:
    state_files = gather_fingerprint(NAMESPACE)

if not (args.gather_cluster_extension_state and args.no_tree):
    for i, uid in enumerate(top_level_kinds):
        print_tree(uid, prefix="", is_last=(i == len(top_level_kinds)-1))

if args.gather_cluster_extension_state:
    if not state_files:
        print("No ClusterExtension found in the namespace, no state file created.", file=sys.stderr)
    else:
        print("Created state file(s):", ", ".join(state_files))

# If --prompt is used, we already created the fingerprint file. Now load and send to OpenAI.
if args.prompt:
    if not state_files:
        print("No ClusterExtension found, cannot prompt OpenAI.", file=sys.stderr)
        sys.exit(1)
    # Assume one ClusterExtension, take the first file
    fingerprint_data = load_fingerprint(state_files[0])
    prompt = generate_prompt(fingerprint_data)
    response = send_to_openai(prompt)
    print("\n--- OpenAI Diagnosis ---\n")
    print(response)