# 🚀 YONACODE ($YGO) 轻量挖矿指南 (LIGHT MINING)
> **远程代理挖矿与 Yona Hash 算法 (区块高度 ≥ 38500) 技术指南**

本文档详细介绍了**什么是轻量挖矿 (Light Mining)**、远程代理架构的核心技术原理、即将到来的 **Yona Hash 算法升级 (区块高度 ≥ 38500)**、完整 CLI 命令与参数参考表，以及 Windows、Linux 和 HiveOS 的操作使用指南。

---

## 💡 1. 什么是轻量挖矿 (LIGHT MINING)？

**轻量挖矿 (Light Mining / Remote Proxy Mining)** 是 YonaCode ($YGO) 推出的新一代挖矿解决方案。

无需矿工下载和存储数十 GB 的区块链账本数据，也无需运行占用大量 SSD 空间和 CPU 算力的全节点，**轻量挖矿将 100% 的本地硬件资源完全集中在 CUDA GPU 算力哈希计算上**。

### 🌟 轻量挖矿的核心优势：
1. **体积超轻量 (< 30 MB)**: 零区块链账本下载，启动即挖，2 秒内即可开始挖矿。
2. **极低资源占用**: 零 SSD/HDD 存储消耗，极低内存占用，无需 CPU 参与区块验证。
3. **CUDA GPU 算力最大化 (2.3+ GH/s)**: NVIDIA/AMD 显卡算力 100% 专注于 GPU CUDA 哈希计算。
4. **直连生产级 VPS 节点**: 轻量矿工自动连接官方 VPS 节点 (`110.172.28.103:8080`) 获取最新区块模板。
5. **奖励直接安全到账**: 出块成功后，区块奖励直接发送至您唯一的 Ed25519 钱包地址 (`0x...`)。

---

## ⚡ 2. 算法升级通知: "YONA HASH" (高度 ≥ 38500)

> [!IMPORTANT]
> **YONA HASH 算法升级通知：**
> 自 **区块高度 ≥ 38500** 起，YonaCode 网络将正式切换至新一代抗 ASIC 的 **Yona Hash** 算法（结合 `parent_hash` 和 `merkle_root`）。
> 
> * **自动平滑无感切换**: 本轻量挖矿版本中的 GPU CUDA 挖矿引擎已预装自动算法热切换逻辑。当节点广播 Height $\ge 38500$ 的区块模板时，矿机将**自动无缝切换至 Yona Hash，无需矿工重启或手动重新配置**。

---

## 🛠️ 3. 完整 CLI 命令行参数参考表 (CLI COMMAND REFERENCE)

| 参数标志 (Flag) | 参数详细说明 | 默认值 | 实际使用示例 |
| :--- | :--- | :--- | :--- |
| **`--wallet`** | 接收 $YGO 奖励的 `0x...` 钱包地址 | *(自动提示或生成)* | `--wallet 0x680303fe12345678...` |
| **`--node`** | 生产级 VPS 节点的 IP 与端口 | `110.172.28.103:8080` | `--node 110.172.28.103:8080` |
| **`--lang`** | CLI 终端界面显示语言 (`vn`, `en`, `zh`) | `vn` | `--lang en` *(英文)* <br> `--lang zh` *(中文)* |
| **`--cli`** | 强制以 CLI 命令行终端模式运行 | `false` | `--cli` |
| **`--device`** | 挖矿硬件设备 (`gpu`) | `gpu` | `--device gpu` |
| **`--port`** | 内部 Web Dashboard 代理端口 | `18888` | `--port 18888` |
| **`--no-browser`** | 启动时不自动打开 Web 浏览器 | `false` | `--no-browser` |
| **`--help`** | 显示完整的帮助信息与参数列表 | `false` | `--help` |

---

## 💻 4. WINDOWS、LINUX 与 HIVEOS 详细运行指南

### A. Windows 系统:

#### 方法 1: 使用 Web UI 仪表板 (GUI)
1. 解压 `YonaCode_Light_Mining_Windows.zip`。
2. 双击运行 `YonaCode_Light_Miner.exe`。
3. 浏览器自动打开 `http://127.0.0.1:18888` $\rightarrow$ 输入钱包地址 $\rightarrow$ 点击 **"启动 GPU CUDA 矿机"**。

#### 方法 2: 使用 CLI 命令行终端 (CLI Terminal)
打开命令提示符 (cmd) 或 PowerShell:
```cmd
# 以英文命令行模式运行
yona_light_miner_cli.exe --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang en

# 指定 VPS 节点地址以中文模式运行
yona_light_miner_cli.exe --cli --node 110.172.28.103:8080 --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang zh
```

---

### B. Linux 终端运行 (CLI):
```bash
# 赋予可执行权限
chmod +x yona_light_miner_cli

# 以中文命令行模式运行
./yona_light_miner_cli --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang zh

# 以英文命令行模式运行
./yona_light_miner_cli --cli --wallet 0x680303fe1234567890abcdef1234567890abcdef --lang en
```

---

### C. HiveOS 矿机一键自动配置:
在 HiveOS 矿机上通过 SSH 或 Hive Shell 运行：
```bash
bash hiveos_setup.sh --node 110.172.28.103:8080
```
*或使用 curl:*
```bash
curl -sSL https://raw.githubusercontent.com/duyvn878-ui/YonaCode/main/hiveos_setup.sh | bash -s -- --node 110.172.28.103:8080
```

---

## 📊 5. 查看实时日志与出块状态

在 Linux/HiveOS 上查看实时挖矿日志：
```bash
tail -f /var/log/miner/custom/yona_gpu_miner/yona_gpu_miner.log
```

切换至 Yona Hash 算法后的日志输出 (Height $\ge 38500$):
```text
[GPU-MINER] 🔨 Mining Block #38505 (Yona Hash) | Speed: 2.31 GH/s | Nonce: 0x4a1f...
[GPU-MINER] 🏆 Success! Found valid solution for Block #38505!
```
您的轻量挖矿系统已 100% 为 Yona Hash 做好准备！
