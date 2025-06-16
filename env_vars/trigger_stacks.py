#!/usr/bin/env python3
"""
Script to convert env vars YAML file to Spacelift runtime config format
for use with "spacectl stack preview --runtime-config"
"""

import yaml
import json
import argparse
import sys
import os
import urllib.request

domain = os.getenv("SPACELIFT_DOMAIN")

def load_yaml_file(file_path):
    """Load YAML file and return parsed content"""
    try:
        with open(file_path, 'r') as file:
            return yaml.safe_load(file)
    except FileNotFoundError:
        print(f"Error: File {file_path} not found", file=sys.stderr)
        sys.exit(1)
    except yaml.YAMLError as e:
        print(f"Error parsing YAML: {e}", file=sys.stderr)
        sys.exit(1)


def convert_to_runtime_config(yaml_data):
    """Convert YAML data to Spacelift runtime config format"""
    runtime_config = {}

    for stack_id, env_vars in yaml_data.items():
        if not isinstance(env_vars, list):
            print(f"Error: Environment variables for stack '{stack_id}' must be a list", file=sys.stderr)
            sys.exit(1)

        runtime_config[stack_id] = {
            "environment": {}
        }

        for var in env_vars:
            runtime_config[stack_id]["environment"][var["name"]] = var["value"]

    return runtime_config

def query_api(query: str, variables: dict = None) -> dict:
    #api_token = os.getenv("SPACELIFT_API_TOKEN")
    api_token = os.getenv("HOME_TOKEN")

    headers = {
        "Content-Type": "application/json",
        "Authorization": f"Bearer {api_token}"
    }

    data = {
        "query": query,
    }

    if variables is not None:
        data["variables"] = variables

    req = urllib.request.Request(f"https://{domain}/graphql", json.dumps(data).encode('utf-8'), headers)
    with urllib.request.urlopen(req) as response:
        resp = json.loads(response.read().decode('utf-8'))

    return resp

def trigger_stack_previews(runtime_config: dict):
    """Trigger stack previews using Spacelift API"""

    for stack_id, env in runtime_config.items():

        # Get the current tracked sha from the stack
        query = "{ stack(id: \"" + stack_id + "\") { trackedCommit { hash } } }"
        response = query_api(query)
        if "errors" in response:
            print("Error fetching stack tracked commit:", response["errors"], file=sys.stderr)
            continue

        # Ensure we have a tracked commit
        try:
            tracked_commit = response["data"]["stack"]["trackedCommit"]["hash"]
        except TypeError:
            tracked_commit = None
        if tracked_commit is None:
            print(f"Stack {stack_id} has no tracked commit. Skipping.", file=sys.stderr)
            continue

        # Trigger the stack preview with the current tracked commit SHA
        query = """
        mutation TriggerStackPreview($stack: ID!, $commitSHA: String!, $runtimeConfig: String!) {
            runTrigger(stack: $stack, commitSha: $commitSHA, runType: PROPOSED, runtimeConfig: { yaml: $runtimeConfig }) {
                id
            }
        }
        """
        
        variables = {
            "stack": stack_id,
            "commitSHA": tracked_commit,
            "runtimeConfig": yaml.dump(env)
        }

        response = query_api(query, variables)
        if "errors" in response:
            print(f"Error triggering stack preview for {stack_id}:", response["errors"], file=sys.stderr)
        else:
            print(f"Triggered stack preview for {stack_id} with commit {tracked_commit}.")
            print(f"https://{domain}/stack/{stack_id}/run/{response['data']['runTrigger']['id']}")
            print()

def main():
    parser = argparse.ArgumentParser(
        description="Convert env vars YAML to Spacelift runtime config"
    )
    parser.add_argument(
        "--file", "-f",
        default="vars.yaml",
        help="Path to YAML file (default: vars.yaml)"
    )
    
    args = parser.parse_args()

    # ensure we are in a proposed run
    if os.getenv("TF_VAR_spacelift_run_type") != "PROPOSED":
        # This script should only be run in a proposed run context.
        sys.exit(0)
    
    # Load YAML data
    yaml_data = load_yaml_file(args.file)
    
    # Convert to runtime config
    runtime_config = convert_to_runtime_config(yaml_data)

    # Trigger stack previews
    trigger_stack_previews(runtime_config)


if __name__ == "__main__":
    main()