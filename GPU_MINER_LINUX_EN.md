# GPU Mining Installation & Run Guide for Linux (Ubuntu/Debian) ⛏️⚡

This guide provides step-by-step instructions on setting up the NVIDIA driver, CUDA toolkit, and compiling/running the YonaCode GPU Miner on Linux systems.

---

## ⚠️ Requirements
1. **Graphics Processing Unit (GPU):** NVIDIA GPU is required (Maxwell, Pascal, Turing, Ampere, Ada Lovelace architectures or newer - e.g., GTX 10xx, RTX 20xx/30xx/40xx, Tesla T4/A100,...). *AMD and Intel GPUs are not supported.*
2. **Operating System:** Ubuntu 20.04 LTS, Ubuntu 22.04 LTS, or Ubuntu 24.04 LTS is recommended.
3. **Permissions:** Administrator access (`sudo` / `root`).

---

## 🛠️ Step 1: Install NVIDIA Driver
To ensure the GPU operates at peak performance and is compatible with CUDA, you must install the official, stable NVIDIA driver.

1. Update the system package lists:
   ```bash
   sudo apt update && sudo apt upgrade -y
   ```

2. Check available NVIDIA driver versions for your hardware:
   ```bash
   sudo ubuntu-drivers devices
   ```

3. Install the recommended NVIDIA driver version (e.g., version 535 or newer):
   ```bash
   sudo apt install -y nvidia-driver-535
   ```
   *(Alternatively, you can auto-install the best driver using: `sudo ubuntu-drivers install`)*

4. **Reboot the system** to apply driver changes:
   ```bash
   sudo reboot
   ```

5. Once rebooted, verify the driver installation:
   ```bash
   nvidia-smi
   ```
   If you see the GPU status table with the driver version, the installation was successful.

---

## 📦 Step 2: Install CUDA Toolkit
The CUDA Toolkit contains the `nvcc` compiler required to compile the C++/CUDA source files.

### Method 1: Install from APT Repository (Easiest)
```bash
sudo apt update
sudo apt install -y nvidia-cuda-toolkit
```

### Method 2: Official Installation from NVIDIA Repository (Recommended for optimal performance)
Go to the [NVIDIA CUDA Downloads](https://developer.nvidia.com/cuda-downloads) page, select Linux -> x86_64 -> Ubuntu, and follow the instructions.
Example for Ubuntu 22.04:
```bash
wget https://developer.download.nvidia.com/compute/cuda/repos/ubuntu2204/x86_64/cuda-keyring_1.1-1_all.deb
sudo dpkg -i cuda-keyring_1.1-1_all.deb
sudo apt update
sudo apt install -y cuda-toolkit
```

Once installed, append CUDA environment variables to your shell profile (`~/.bashrc`):
```bash
echo 'export PATH=/usr/local/cuda/bin${PATH:+:${PATH}}' >> ~/.bashrc
echo 'export LD_LIBRARY_PATH=/usr/local/cuda/lib64${LD_LIBRARY_PATH:+:${LD_LIBRARY_PATH}}' >> ~/.bashrc
source ~/.bashrc
```

Verify that the `nvcc` compiler is available:
```bash
nvcc --version
```

---

## 🏗️ Step 3: Install Dependencies & Build the GPU Miner
To compile the C++ code, you need to install CMake and build tools (`build-essential`).

1. Install GCC, G++, and CMake:
   ```bash
   sudo apt update
   sudo apt install -y build-essential cmake
   ```

2. Navigate to the GPU Miner directory:
   ```bash
   cd 8_miner_gpu
   ```

3. Create and go into a dedicated build folder:
   ```bash
   mkdir -p build && cd build
   ```

4. Configure the build with CMake:
   ```bash
   cmake -DCMAKE_BUILD_TYPE=Release ..
   ```

5. Compile the miner:
   ```bash
   make -j$(nproc)
   ```
   *After completion, the executable `yona_gpu_miner` will be generated inside the `build/` directory.*

---

## 🚀 Step 4: Run the GPU Miner

YonaCode supports two ways to mine with your GPU:

### Method 1: Automatically run GPU Miner with Node (Recommended)
This is the simplest way. When starting the main Node (`YonaCode`), you only need to include the `--mining` flag and specify the device via the `--mining-device gpu` flag (or `--mining-device hybrid` if you want to mine using both CPU and GPU). The system will automatically spawn the `yona_gpu_miner` process in the background.

Run the following command:
```bash
./YonaCode node start --mining --mining-device gpu --reward-address <YOUR_WALLET_ADDRESS> --miner-pin <YOUR_PIN>
```
* **--mining**: Enables PoW mining.
* **--mining-device**: Selects the mining device: `gpu` (GPU only), `hybrid` (CPU + GPU combined), or `cpu` (CPU only, default).
* **--reward-address**: Your 32-byte Hex wallet address to receive block rewards.
* **--miner-pin**: The security PIN for your wallet.

### Method 2: Standalone GPU Miner Execution
If you run the Node on one machine and want to use a GPU on a different machine, you can run the GPU Miner as a standalone client and connect to the Node remotely.

1. **Verify CUDA Compatibility:**
   ```bash
   ./yona_gpu_miner --check
   ```
   *If successful, it will display `[CUDA-SUCCESS] CUDA is fully operational.`.*

2. **View CLI Help:**
   ```bash
   ./yona_gpu_miner --help
   ```

3. **Run Standalone Miner connected to a remote Node:**
   ```bash
   ./yona_gpu_miner [NODE_IP] [RPC_PORT]
   ```
   * **NODE_IP**: The IP address of the YonaCode Node (Default: `127.0.0.1`).
   * **RPC_PORT**: The RPC port of the Node (Default: `8080`).

   Example connecting to a Node at `192.168.1.100` on port `8080`:
   ```bash
   ./yona_gpu_miner 192.168.1.100 8080
   ```

### 3. Run in the Background
To run the miner in the background so it continues when you disconnect from the SSH session, use `screen` or `nohup`:
```bash
# Install screen
sudo apt install -y screen

# Create a new screen session named 'miner'
screen -S miner

# Launch the miner
./yona_gpu_miner 127.0.0.1 8080

# Press 'Ctrl + A', then press 'D' to detach the screen session.
# To reconnect to the mining monitor later:
screen -r miner
```
