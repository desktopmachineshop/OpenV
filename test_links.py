#!/usr/bin/env python3
"""
Test script to debug link auto-versioning issue.
"""
import requests
import json
import time

BASE_URL = "http://localhost:8080/api/v1"

# First, get or create a project
print("Getting projects...")
projects_resp = requests.get(f"{BASE_URL}/projects")
projects = projects_resp.json()
if projects:
    project_id = projects[0]["id"]
    print(f"Using project: {project_id}")
else:
    print("No projects found")
    exit(1)

# Get existing artifacts to see what's there
print("\nGetting artifacts...")
artifacts_resp = requests.get(f"{BASE_URL}/artifacts", params={"project_id": project_id})
artifacts = artifacts_resp.json() if isinstance(artifacts_resp.json(), list) else artifacts_resp.json().get("artifacts", [])
print(f"Found {len(artifacts)} artifacts")

# List them with their versions
for art in artifacts[:5]:
    print(f"  - {art['id'][:8]}: {art['title']} (v{art['version']})")

# Create two new test artifacts
print("\nCreating test artifacts...")
test1_payload = {
    "type": "requirement",
    "title": "test1",
    "body": "First test artifact",
    "attributes": {}
}
test1_resp = requests.post(f"{BASE_URL}/artifacts", json=test1_payload)
test1 = test1_resp.json()
test1_id = test1["id"]
print(f"Created test1: {test1_id} (v{test1['version']})")

# Also check if req1 exists, if not create it
req1 = None
for art in artifacts:
    if art["title"] == "req1":
        req1 = art
        break

if not req1:
    print("Creating req1...")
    req1_payload = {
        "type": "requirement",
        "title": "req1",
        "body": "First requirement",
        "attributes": {}
    }
    req1_resp = requests.post(f"{BASE_URL}/artifacts", json=req1_payload)
    req1 = req1_resp.json()
    print(f"Created req1: {req1['id']} (v{req1['version']})")
else:
    print(f"Using existing req1: {req1['id']} (v{req1['version']})")

req1_id = req1["id"]

# Now update test1 and add a link to req1
print(f"\nUpdating test1 with a link to req1...")
print(f"  test1 ID: {test1_id}")
print(f"  req1 ID: {req1_id}")

update_payload = {
    "type": "requirement",
    "title": "test1",
    "body": "First test artifact - updated",
    "attributes": {},
    "pendingLinkAdds": [
        {
            "from_id": test1_id,
            "to_id": req1_id,
            "type": "verifies",
            "attributes": {}
        }
    ],
    "pendingLinkRemoves": []
}

print(f"Payload: {json.dumps(update_payload, indent=2)}")

update_resp = requests.put(f"{BASE_URL}/artifacts/{test1_id}", json=update_payload)
if update_resp.status_code == 200:
    updated_test1 = update_resp.json()
    print(f"✓ test1 updated: v{updated_test1['version']} (was v{test1['version']})")
else:
    print(f"✗ Update failed: {update_resp.status_code} - {update_resp.text}")

# Wait a moment for any async operations
time.sleep(2)

# Check the versions now
print("\nChecking versions after update...")
test1_check = requests.get(f"{BASE_URL}/artifacts/{test1_id}").json()
req1_check = requests.get(f"{BASE_URL}/artifacts/{req1_id}").json()

print(f"test1: v{test1_check['version']} (updated: {test1_check['version'] > test1['version']})")
print(f"req1: v{req1_check['version']} (updated: {req1_check['version'] > req1['version']})")

if req1_check['version'] == req1['version']:
    print("\n✗ ISSUE: req1 did not auto-version!")
else:
    print("\n✓ req1 was auto-versioned correctly")
