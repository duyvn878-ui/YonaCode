/**
 * @file main.go
 * @brief Công cụ Khởi chạy & Tự động Cài đặt Driver GPU (NVIDIA CUDA / AMD ROCm)
 * @date 22/07/2026
 * @mô_tả:
 *   - Tự động kiểm tra CUDA / ROCm / OpenCL API trên Windows và Linux.
 *   - Quét PCI Bus (lspci / WMI / PNPDeviceID) phát hiện phần cứng card đồ họa NVIDIA hoặc AMD.
 *   - Tự động thực thi lệnh hệ thống cài đặt driver (nvidia-driver, cuda-toolkit, rocm, opencl).
 *   - Hiển thị thông báo và gửi lệnh khởi động lại hệ thống (Reboot) sau khi hoàn tất.
 */

package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)


// Global Configuration
const (
	VendorIDNvidia = "10de"
	VendorIDAmd    = "1002"
)

type GPUVendor string

const (
	VendorNvidia GPUVendor = "NVIDIA"
	VendorAMD    GPUVendor = "AMD"
	VendorUnknown GPUVendor = "UNKNOWN"
)

type SystemInfo struct {
	OS          string
	Distro      string
	IsAdmin     bool
	HasCUDADriver  bool
	HasROCmDriver  bool
	DetectedGPUs   []GPUVendor
}

func main() {
	fmt.Println("=======================================================================")
	fmt.Println("       🚀 YONACODE GPU DRIVER AUTO-INSTALLER & HARDWARE DETECTOR       ")
	fmt.Println("=======================================================================")
	fmt.Printf("OS: %s | Arch: %s\n\n", runtime.GOOS, runtime.GOARCH)

	sysInfo := &SystemInfo{
		OS: runtime.GOOS,
	}

	// Step 1: Check Administrator / Privileges
	sysInfo.IsAdmin = checkAdminPrivileges()
	if !sysInfo.IsAdmin {
		fmt.Println("⚠️ [LƯU Ý]: Bạn chưa chạy chương trình với quyền Quản trị viên (Admin/Root).")
		fmt.Println("   Nếu cần tự động cài đặt driver mới, hãy khởi chạy lại bằng 'Run as Administrator' hoặc 'sudo'.")
		fmt.Println("-----------------------------------------------------------------------")
	}

	// Step 2: Check Existing CUDA / ROCm / OpenCL API
	fmt.Println("🔍 [BƯỚC 1]: Kiểm tra API CUDA / ROCm / OpenCL hiện tại...")
	sysInfo.HasCUDADriver = checkCUDADriverAvailable()
	sysInfo.HasROCmDriver = checkROCmDriverAvailable()

	if sysInfo.HasCUDADriver {
		fmt.Println("✅ [THÀNH CÔNG]: Đã phát hiện Driver CUDA / NVIDIA đang hoạt động chuẩn xác!")
	}
	if sysInfo.HasROCmDriver {
		fmt.Println("✅ [THÀNH CÔNG]: Đã phát hiện Driver ROCm / OpenCL AMD đang hoạt động chuẩn xác!")
	}

	if sysInfo.HasCUDADriver || sysInfo.HasROCmDriver {
		fmt.Println("\n🎉 [KẾT QUẢ]: Hệ thống đã có sẵn Driver GPU tăng tốc khai thác!")
		fmt.Println("   Bạn có thể khởi chạy đào coin GPU (yona_gpu_miner) ngay lập tức.")
		fmt.Println("=======================================================================")
		waitExit()
		return
	}

	fmt.Println("\n❌ [THÔNG BÁO]: Chưa phát hiện Driver CUDA hoặc ROCm API hỗ trợ tăng tốc GPU.")

	// Step 3: Scan PCI Bus & Hardware Devices
	fmt.Println("\n🔍 [BƯỚC 2]: Tiến hành quét PCI Bus & thiết bị phần cứng GPU...")
	sysInfo.DetectedGPUs = scanGPUDevices(sysInfo.OS)

	if len(sysInfo.DetectedGPUs) == 0 {
		fmt.Println("⚠️ [CẢNH BÁO]: Không tìm thấy Card đồ họa rời NVIDIA hoặc AMD nào trên hệ thống.")
		fmt.Println("   Vui lòng kiểm tra lại cáp nối phần cứng hoặc thiết bị GPU của bạn.")
		fmt.Println("=======================================================================")
		waitExit()
		return
	}

	hasNvidia := false
	hasAmd := false
	for _, v := range sysInfo.DetectedGPUs {
		if v == VendorNvidia {
			hasNvidia = true
			fmt.Println("  📌 Phát hiện: Card đồ họa NVIDIA (Vendor ID: 0x10DE)")
		} else if v == VendorAMD {
			hasAmd = true
			fmt.Println("  📌 Phát hiện: Card đồ họa AMD Radeon (Vendor ID: 0x1002)")
		}
	}

	// Step 4: Execute Driver Installation
	fmt.Println("\n🛠️ [BƯỚC 3]: Thực thi lệnh hệ thống tự động cài đặt Driver & CUDA Toolkit...")

	if !sysInfo.IsAdmin {
		fmt.Println("❌ [CẦN QUYỀN ADMIN]: Vui lòng chạy ứng dụng với quyền Administrator/Root để tiến hành cài đặt.")
		waitExit()
		return
	}

	installed := false
	if hasNvidia {
		installed = installNvidiaDriver(sysInfo.OS)
	}
	if hasAmd {
		installed = installAmdDriver(sysInfo.OS) || installed
	}

	// Step 5: Reboot Notification
	if installed {
		fmt.Println("\n=======================================================================")
		fmt.Println("🎉 [THÀNH CÔNG]: ĐÃ CÀI ĐẶT DRIVER GPU HOÀN TẤT!")
		fmt.Println("⚠️ [YÊU CẦU KHỞI ĐỘNG LẠI]: Hệ thống cần Reboot để nạp Driver Module vào Kernel.")
		fmt.Println("=======================================================================")
		promptAndReboot(sysInfo.OS)
	} else {
		fmt.Println("\n❌ [LỖI]: Không thể cài đặt tự động. Vui lòng cài thủ công Driver từ trang chủ NVIDIA/AMD.")
		waitExit()
	}
}

// Check if running as Admin (Windows) or Root (Linux)
func checkAdminPrivileges() bool {
	if runtime.GOOS == "windows" {
		cmd := exec.Command("net", "session")
		err := cmd.Run()
		return err == nil
	} else {
		return os.Geteuid() == 0
	}
}

// Check CUDA Driver availability
func checkCUDADriverAvailable() bool {
	// Try running nvidia-smi
	cmd := exec.Command("nvidia-smi")
	if err := cmd.Run(); err == nil {
		return true
	}

	// Check CUDA compiler or libraries
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = "C:\\Windows"
		}
		cudaDll := filepath.Join(systemRoot, "System32", "nvcuda.dll")
		if _, err := os.Stat(cudaDll); err == nil {
			return true
		}
	} else {
		if _, err := os.Stat("/dev/nvidia0"); err == nil {
			return true
		}
		if _, err := exec.LookPath("nvcc"); err == nil {
			return true
		}
	}
	return false
}

// Check ROCm Driver availability
func checkROCmDriverAvailable() bool {
	if _, err := exec.LookPath("rocm-smi"); err == nil {
		return true
	}
	if _, err := exec.LookPath("clinfo"); err == nil {
		return true
	}
	if runtime.GOOS == "windows" {
		systemRoot := os.Getenv("SystemRoot")
		if systemRoot == "" {
			systemRoot = "C:\\Windows"
		}
		openclDll := filepath.Join(systemRoot, "System32", "OpenCL.dll")
		if _, err := os.Stat(openclDll); err == nil {
			return true
		}
	} else {
		if _, err := os.Stat("/dev/kfd"); err == nil {
			return true
		}
	}
	return false
}

// Scan PCI Bus / Hardware Devices
func scanGPUDevices(osName string) []GPUVendor {
	vendorsMap := make(map[GPUVendor]bool)

	if osName == "windows" {
		// Use PowerShell Get-CimInstance Win32_VideoController
		out, err := exec.Command("powershell", "-NoProfile", "-Command", "Get-CimInstance Win32_VideoController | Select-Object Name, PNPDeviceID | Format-Table -HideTableHeaders").Output()
		if err == nil {
			text := strings.ToUpper(string(out))
			if strings.Contains(text, "NVIDIA") || strings.Contains(text, "VEN_10DE") {
				vendorsMap[VendorNvidia] = true
			}
			if strings.Contains(text, "AMD") || strings.Contains(text, "RADEON") || strings.Contains(text, "ATI") || strings.Contains(text, "VEN_1002") {
				vendorsMap[VendorAMD] = true
			}
		} else {
			// Fallback WMIC
			wmicOut, werr := exec.Command("wmic", "path", "win32_videocontroller", "get", "name,pnpdeviceid").Output()
			if werr == nil {
				text := strings.ToUpper(string(wmicOut))
				if strings.Contains(text, "NVIDIA") || strings.Contains(text, "VEN_10DE") {
					vendorsMap[VendorNvidia] = true
				}
				if strings.Contains(text, "AMD") || strings.Contains(text, "RADEON") || strings.Contains(text, "ATI") || strings.Contains(text, "VEN_1002") {
					vendorsMap[VendorAMD] = true
				}
			}
		}
	} else {
		// Linux: lspci -nn
		out, err := exec.Command("lspci", "-nn").Output()
		if err == nil {
			lines := strings.Split(string(out), "\n")
			for _, line := range lines {
				lineLower := strings.ToLower(line)
				if strings.Contains(lineLower, "vga") || strings.Contains(lineLower, "3d") || strings.Contains(lineLower, "display") {
					if strings.Contains(lineLower, "10de:") || strings.Contains(lineLower, "nvidia") {
						vendorsMap[VendorNvidia] = true
					}
					if strings.Contains(lineLower, "1002:") || strings.Contains(lineLower, "amd") || strings.Contains(lineLower, "radeon") {
						vendorsMap[VendorAMD] = true
					}
				}
			}
		}
	}

	var result []GPUVendor
	for v := range vendorsMap {
		result = append(result, v)
	}
	return result
}

// Install NVIDIA Driver
func installNvidiaDriver(osName string) bool {
	fmt.Println("🚀 [NVIDIA]: Đang tiến hành cài đặt Nvidia Driver & CUDA Toolkit...")

	if osName == "windows" {
		// Try winget
		if _, err := exec.LookPath("winget"); err == nil {
			fmt.Println("📦 Calling Windows Package Manager (winget)...")
			cmd := exec.Command("winget", "install", "--id", "Nvidia.DisplayDriver", "--accept-package-agreements", "--accept-source-agreements", "--silent")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				return true
			}
		}
		fmt.Println("💡 Tip: Bạn cũng có thể tải bộ cài driver NVIDIA mới nhất tại https://www.nvidia.com/Download/index.aspx")
		return false
	} else {
		// Linux: Detect Distro
		distro := detectLinuxDistro()
		fmt.Printf("📦 Phát hiện bản phân phối Linux: %s\n", distro)

		switch distro {
		case "ubuntu", "debian", "pop", "mint":
			fmt.Println("Executing: apt-get update && apt-get install -y nvidia-driver-535 nvidia-cuda-toolkit")
			cmd := exec.Command("sh", "-c", "apt-get update && apt-get install -y nvidia-driver-535 nvidia-cuda-toolkit || ubuntu-drivers install")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run() == nil
		case "fedora", "rhel", "centos":
			fmt.Println("Executing: dnf install -y akmod-nvidia xorg-x11-drv-nvidia-cuda")
			cmd := exec.Command("sh", "-c", "dnf install -y akmod-nvidia xorg-x11-drv-nvidia-cuda cuda-toolkit")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run() == nil
		case "arch", "manjaro":
			fmt.Println("Executing: pacman -Sy --noconfirm nvidia nvidia-utils cuda")
			cmd := exec.Command("sh", "-c", "pacman -Sy --noconfirm nvidia nvidia-utils cuda")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run() == nil
		default:
			fmt.Println("Executing generic package installation...")
			cmd := exec.Command("sh", "-c", "apt-get update && apt-get install -y nvidia-driver-535 nvidia-cuda-toolkit")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run() == nil
		}
	}
}

// Install AMD Driver
func installAmdDriver(osName string) bool {
	fmt.Println("🚀 [AMD]: Đang tiến hành cài đặt AMD ROCm / OpenCL Driver...")

	if osName == "windows" {
		if _, err := exec.LookPath("winget"); err == nil {
			fmt.Println("📦 Calling Windows Package Manager (winget)...")
			cmd := exec.Command("winget", "install", "--id", "AMD.Software", "--accept-package-agreements", "--accept-source-agreements", "--silent")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			if err := cmd.Run(); err == nil {
				return true
			}
		}
		fmt.Println("💡 Tip: Bạn có thể tải bộ cài AMD Radeon Software tại https://www.amd.com/en/support")
		return false
	} else {
		distro := detectLinuxDistro()
		fmt.Printf("📦 Phát hiện bản phân phối Linux: %s\n", distro)

		switch distro {
		case "ubuntu", "debian", "pop", "mint":
			fmt.Println("Executing: apt-get update && apt-get install -y rocm-opencl-runtime rocm-hip-sdk")
			cmd := exec.Command("sh", "-c", "apt-get update && apt-get install -y rocm-opencl-runtime rocm-hip-sdk")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run() == nil
		case "fedora", "rhel", "centos":
			fmt.Println("Executing: dnf install -y rocm-opencl-devel rocm-hip-devel")
			cmd := exec.Command("sh", "-c", "dnf install -y rocm-opencl-devel rocm-hip-devel")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run() == nil
		case "arch", "manjaro":
			fmt.Println("Executing: pacman -Sy --noconfirm opencl-amd rocm-hip-sdk")
			cmd := exec.Command("sh", "-c", "pacman -Sy --noconfirm opencl-amd rocm-hip-sdk")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run() == nil
		default:
			cmd := exec.Command("sh", "-c", "apt-get update && apt-get install -y rocm-opencl-runtime rocm-hip-sdk")
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			return cmd.Run() == nil
		}
	}
}

// Detect Linux Distribution from /etc/os-release
func detectLinuxDistro() string {
	file, err := os.Open("/etc/os-release")
	if err != nil {
		return "unknown"
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "ID=") {
			id := strings.TrimPrefix(line, "ID=")
			id = strings.Trim(id, "\"")
			return strings.ToLower(id)
		}
	}
	return "unknown"
}

// Prompt User & Reboot System
func promptAndReboot(osName string) {
	fmt.Print("\n⚠️  Bạn có muốn KHỞI ĐỘNG LẠI (Reboot) máy tính ngay bây giờ không? (Y/n): ")
	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(strings.ToLower(input))

	if input == "" || input == "y" || input == "yes" {
		fmt.Println("🔄 Đang gửi lệnh Reboot máy tính sau 10 giây...")
		if osName == "windows" {
			exec.Command("shutdown", "/r", "/t", "10", "/c", "YonaCode GPU Driver Setup complete. Rebooting...").Run()
		} else {
			exec.Command("shutdown", "-r", "+1", "YonaCode GPU Driver Setup complete. Rebooting...").Run()
		}
	} else {
		fmt.Println("ℹ️ Vui lòng tự khởi động lại máy tính trước khi khởi chạy đào GPU.")
	}
}

func waitExit() {
	fmt.Print("\nNhấn Enter để thoát...")
	bufio.NewReader(os.Stdin).ReadString('\n')
}
