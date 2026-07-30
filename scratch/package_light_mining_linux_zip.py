# -*- coding: utf-8 -*-
import os
import zipfile
import shutil

root_dir = r"d:\hanhtrinhhocta-p\sssd\BTC"
staging_dir = os.path.join(root_dir, "bbuild", "11_light_mining_linux")
zip_dir = os.path.join(root_dir, "zip")
bbuild_dir = os.path.join(root_dir, "bbuild")

if not os.path.exists(zip_dir):
    os.makedirs(zip_dir)

zip_output = os.path.join(zip_dir, "YonaCode_Light_Mining_Linux.zip")

# Generate 1-click run script for Linux with auto-chmod
run_sh_content = """#!/bin/bash
chmod +x yona_light_miner_linux yona_light_miner_cli yona_gpu_miner genz_miner hiveos_setup.sh
./yona_light_miner_linux
"""
run_sh_path = os.path.join(staging_dir, "run_light_mining.sh")
with open(run_sh_path, "w", newline="\n") as f:
    f.write(run_sh_content)

files_to_pack = [
    ("yona_light_miner_linux", os.path.join(staging_dir, "yona_light_miner_linux")),
    ("yona_light_miner_cli", os.path.join(staging_dir, "yona_light_miner_cli")),
    ("yona_gpu_miner", os.path.join(root_dir, "bin", "linux", "yona_gpu_miner")),
    ("genz_miner", os.path.join(root_dir, "bin", "linux", "genz_miner")),
    ("hiveos_setup.sh", os.path.join(staging_dir, "hiveos_setup.sh")),
    ("run_light_mining.sh", run_sh_path),
]

print(f"Creating Linux Zip Archive: {zip_output} ...")
with zipfile.ZipFile(zip_output, "w", zipfile.ZIP_DEFLATED) as z:
    for archive_name, full_path in files_to_pack:
        if os.path.exists(full_path):
            z.write(full_path, arcname=archive_name)
            print(f"  + Added: {archive_name}")
        else:
            print(f"  - Warning: Missing file {full_path}")

shutil.copy(zip_output, os.path.join(bbuild_dir, "YonaCode_Light_Mining_Linux.zip"))
print(f"[SUCCESS] Package saved to: {zip_output}")
