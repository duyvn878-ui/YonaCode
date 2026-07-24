# GPU DRIVER INSTALLATION GUIDE (MANUAL SETUP)
> Manual installation and troubleshooting guide for NVIDIA (CUDA) and AMD (ROCm/OpenCL) drivers on Windows and Linux when the automated installer (`yona_gpu_setup`) fails due to privilege or package manager issues.

---

## 🛡️ SECTION 1: NVIDIA GRAPHICS CARDS (CUDA TOOLKIT)

### 1. Windows Operating System
If the automated command is blocked by Windows UAC or system policies, follow this sequence:

1. **Automated Installation via CLI (Recommended)**:
   Open PowerShell as Administrator and execute the following commands to download and install everything silently:
   ```powershell
   # Silently install NVIDIA GeForce Experience (to keep the display driver up-to-date)
   winget install --id Nvidia.GeForceExperience --silent --accept-package-agreements --accept-source-agreements

   # Silently install NVIDIA CUDA Toolkit
   winget install --id Nvidia.CUDA --silent --accept-package-agreements --accept-source-agreements
   ```

2. **Manual Installation (Fallback if CLI fails)**:
   - **NVIDIA Display Driver**: Download directly from the official [NVIDIA Driver Downloads](https://www.nvidia.com/Download/index.aspx) portal.
   - **CUDA Toolkit (Requires version 11.8 to 12.x)**: Download directly from [NVIDIA CUDA Toolkit Archive](https://developer.nvidia.com/cuda-toolkit-archive) (CUDA 12.2 or 12.4 is highly recommended). Download the `exe (local)` installer and install in **Express** mode.

3. **Verify Status**:
   - Open PowerShell or Command Prompt and run:
     ```cmd
     nvidia-smi
     ```
   - If it displays details about your GPU and target CUDA driver version, the setup is successful.

---

### 2. Linux Operating System
Make sure to execute all packages under root privileges or using the `sudo` prefix.

#### A. Ubuntu / Debian Distributions
```bash
# 1. Update package index
sudo apt-get update

# 2. Install recommended Nvidia Driver (version 535 or newer)
sudo apt-get install -y nvidia-driver-535 nvidia-utils-535

# 3. Install CUDA Development Toolkit
sudo apt-get install -y nvidia-cuda-toolkit
```

#### B. Fedora / RedHat / CentOS Distributions
```bash
# 1. Add RPM Fusion repositories (if not already added)
sudo dnf install -y https://mirrors.rpmfusion.org/free/fedora/rpmfusion-free-release-$(rpm -E %fedora).noarch.rpm \
                    https://mirrors.rpmfusion.org/nonfree/fedora/rpmfusion-nonfree-release-$(rpm -E %fedora).noarch.rpm

# 2. Install Nvidia Drivers and CUDA Toolkit
sudo dnf clean all
sudo dnf install -y akmod-nvidia xorg-x11-drv-nvidia-cuda cuda-toolkit
```

#### C. Arch Linux / Manjaro Distributions
```bash
# Install proprietary NVIDIA graphics driver and CUDA Toolkit
sudo pacman -Syu --noconfirm nvidia nvidia-utils cuda
```

**⚠️ Important Linux Note**: You must restart your computer using `sudo reboot` for the system to load the driver modules into the active kernel.

---

## ⚡ SECTION 2: AMD GRAPHICS CARDS (ROCm / OPENCL)

### 1. Windows Operating System
Windows does not officially support AMD's ROCm SDK (ROCm is exclusive to Linux). The Yona GPU miner will instead fall back to the **OpenCL API** for mining:

1. **Automated Installation via CLI (Recommended)**:
   Open PowerShell as Administrator and run the following command to download and install AMD Adrenalin silently (includes OpenCL runtime):
   ```powershell
   winget install --id AMD.Adrenalin --silent --accept-package-agreements --accept-source-agreements
   ```

2. **Manual Installation (Fallback if CLI fails)**:
   - Download the latest **AMD Software: Adrenalin Edition** installer from the official [AMD Drivers & Support](https://www.amd.com/en/support) portal.
   - Run the `.exe` installer to automatically install the graphics driver and compile OpenCL runtimes.
   - Verify that the `OpenCL.dll` library is present in your `C:\Windows\System32\`.

---

### 2. Linux Operating System (Ubuntu / Debian / Arch)
AMD offers **ROCm** and the **HIP SDK** to maximize compute efficiency on Linux.

#### A. Official AMDGPU Installer (Recommended for Ubuntu/Debian)
```bash
# 1. Download the AMDGPU setup package
wget https://repo.radeon.com/amdgpu-install/6.1.2/ubuntu/jammy/amdgpu-install_6.1.60102-1_all.deb

# 2. Install the AMD package manager
sudo apt-get install ./amdgpu-install_6.1.60102-1_all.deb

# 3. Trigger setup for ROCm and OpenCL runtimes for GPU mining
sudo amdgpu-install --usecase=rocm,opencl -y
```

#### B. Arch Linux Setup
```bash
# Install AMD OpenCL runtimes and HIP SDK packages
sudo pacman -Syu --noconfirm opencl-amd rocm-hip-sdk clinfo
```

---

## 🔍 TROUBLESHOOTING & VERIFICATION

### 1. "Secure Boot" Driver Lockout on Linux
If the driver package installs successfully but executing `nvidia-smi` or `rocm-smi` returns a connection/load error:
* **Root Cause**: The **Secure Boot** feature in your BIOS/UEFI blocks the kernel from loading unsigned third-party driver modules.
* **Resolution**:
  1. Restart your PC, press keys (F2, F12, or Del) to enter your BIOS/UEFI Settings interface.
  2. Locate the **Secure Boot** policy option and set it to **Disabled**.
  3. Save configurations (F10) and boot back into Linux.

### 2. Verify GPU Hardware Detection
Run these commands to confirm that the system registers the physical graphics card:
- **Windows**: `wmic path win32_videocontroller get name`
- **Linux**: `lspci | grep -E "VGA|3D|Display"`
