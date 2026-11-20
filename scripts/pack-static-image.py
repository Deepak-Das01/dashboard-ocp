#!/usr/bin/env python3
"""Pack a statically linked binary into a scratch docker-archive for: ctr images import.

Usage:
  pack-static-image.py <binary> <image:tag> <out.tar> [--entrypoint /manager]
"""
from __future__ import annotations

import hashlib
import json
import os
import sys
import tarfile
import tempfile
import time


def sha256_file(path: str) -> str:
    h = hashlib.sha256()
    with open(path, "rb") as f:
        for chunk in iter(lambda: f.read(1024 * 1024), b""):
            h.update(chunk)
    return h.hexdigest()


def sha256_bytes(data: bytes) -> str:
    return hashlib.sha256(data).hexdigest()


def main() -> int:
    if len(sys.argv) < 4:
        print(__doc__, file=sys.stderr)
        return 2
    binary = sys.argv[1]
    image = sys.argv[2]
    out_tar = sys.argv[3]
    entrypoint = "/manager"
    if "--entrypoint" in sys.argv:
        entrypoint = sys.argv[sys.argv.index("--entrypoint") + 1]

    if ":" not in image:
        print("image must be name:tag", file=sys.stderr)
        return 2
    name, tag = image.rsplit(":", 1)
    arcname = entrypoint.lstrip("/") or "manager"

    with tempfile.TemporaryDirectory(prefix="pack-img-") as tmp:
        layer_tar_path = os.path.join(tmp, "layer.tar")
        with tarfile.open(layer_tar_path, "w") as tf:
            info = tarfile.TarInfo(name=arcname)
            info.mode = 0o755
            info.uid = 0
            info.gid = 0
            info.mtime = int(time.time())
            with open(binary, "rb") as bf:
                data = bf.read()
            info.size = len(data)
            import io

            tf.addfile(info, io.BytesIO(data))

        layer_digest = sha256_file(layer_tar_path)
        layer_size = os.path.getsize(layer_tar_path)
        created = time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

        config = {
            "architecture": "amd64",
            "os": "linux",
            "config": {
                "Entrypoint": [entrypoint],
                "Env": ["PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"],
                "WorkingDir": "/",
                "User": "65532:65532",
            },
            "rootfs": {"type": "layers", "diff_ids": [f"sha256:{layer_digest}"]},
            "history": [{"created": created, "created_by": "pack-static-image.py"}],
            "created": created,
        }
        config_bytes = json.dumps(config, separators=(",", ":"), sort_keys=True).encode()
        config_digest = sha256_bytes(config_bytes)
        config_name = config_digest + ".json"
        with open(os.path.join(tmp, config_name), "wb") as f:
            f.write(config_bytes)

        layer_dir_name = layer_digest
        os.makedirs(os.path.join(tmp, layer_dir_name), exist_ok=True)
        os.rename(layer_tar_path, os.path.join(tmp, layer_dir_name, "layer.tar"))
        with open(os.path.join(tmp, layer_dir_name, "VERSION"), "w") as f:
            f.write("1.0")
        with open(os.path.join(tmp, layer_dir_name, "json"), "w") as f:
            json.dump({"id": layer_digest}, f)

        with open(os.path.join(tmp, "manifest.json"), "w") as f:
            json.dump(
                [
                    {
                        "Config": config_name,
                        "RepoTags": [image],
                        "Layers": [f"{layer_dir_name}/layer.tar"],
                    }
                ],
                f,
            )
        with open(os.path.join(tmp, "repositories"), "w") as f:
            json.dump({name: {tag: layer_digest}}, f)

        os.makedirs(os.path.dirname(out_tar) or ".", exist_ok=True)
        with tarfile.open(out_tar, "w") as out:
            for root, _dirs, files in os.walk(tmp):
                for fn in files:
                    full = os.path.join(root, fn)
                    out.add(full, arcname=os.path.relpath(full, tmp))

        print(f"packed {image} ({layer_size} layer bytes) → {out_tar}")
    return 0


if __name__ == "__main__":
    raise SystemExit(main())
