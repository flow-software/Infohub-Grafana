"""Zip dist/ into installable plugin packages, one per platform plus "any".

Same layout as a Grafana catalog release: each zip holds one top-level folder
named after the plugin ID, so it unzips straight into Grafana's plugins
directory. Run after a full build (npm run build, then mage -v):

    docker run --rm -v "${PWD}:/src" -w /src python:3-alpine python scripts/package.py
"""

import hashlib
import json
import os
import zipfile

PLUGIN_ID = "flowsoftware-flow-datasource"
DIST, OUT = "dist", "artifacts"


def add_file(z, full, rel):
    info = zipfile.ZipInfo.from_file(full, f"{PLUGIN_ID}/{rel}")
    # Set modes explicitly: files built on a Windows bind mount do not carry
    # reliable permissions, and Grafana must be able to execute the backend.
    mode = 0o755 if rel.startswith("gpx_") else 0o644
    info.external_attr = (0o100000 | mode) << 16
    info.compress_type = zipfile.ZIP_DEFLATED
    with open(full, "rb") as fh:
        z.writestr(info, fh.read())


def add(z, rel):
    path = os.path.join(DIST, rel)
    if not os.path.isdir(path):
        add_file(z, path, rel)
        return
    for root, _, names in os.walk(path):
        for name in names:
            full = os.path.join(root, name)
            add_file(z, full, os.path.relpath(full, DIST).replace(os.sep, "/"))


def main():
    version = json.load(open(os.path.join(DIST, "plugin.json")))["info"]["version"]
    names = sorted(os.listdir(DIST))
    binaries = [n for n in names if n.startswith("gpx_")]
    if not binaries:
        raise SystemExit("no backend binaries in dist/; run mage -v first")
    common = [n for n in names if n not in binaries and n != "go_plugin_build_manifest" and not n.endswith(".map")]

    # gpx_flow_windows_amd64.exe -> windows_amd64
    targets = {b.split("_", 2)[2].removesuffix(".exe"): [b] for b in binaries}
    targets["any"] = binaries

    os.makedirs(OUT, exist_ok=True)
    sums = []
    for target, bins in targets.items():
        out = os.path.join(OUT, f"{PLUGIN_ID}-{version}.{target}.zip")
        with zipfile.ZipFile(out, "w") as z:
            for name in common + bins:
                add(z, name)
            add_file(z, "INSTALL.md", "INSTALL.md")
        digest = hashlib.sha256(open(out, "rb").read()).hexdigest()
        sums.append(f"{digest}  {os.path.basename(out)}")
        print(f"{os.path.getsize(out) / 1e6:6.1f} MB  {os.path.basename(out)}")

    with open(os.path.join(OUT, "SHA256SUMS.txt"), "w") as fh:
        fh.write("\n".join(sums) + "\n")


if __name__ == "__main__":
    main()
