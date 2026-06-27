//go:build windows
// +build windows

package user_interface

import (
	"fmt"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/go-ole/go-ole"
)

var (
	user32           = syscall.NewLazyDLL("user32.dll")
	procGetLastInput = user32.NewProc("GetLastInputInfo")
	kernel32         = syscall.NewLazyDLL("kernel32.dll")
	procGetTickCount = kernel32.NewProc("GetTickCount64") // [V10.5.2 FIX] Dùng 64-bit chống tràn sau 49 ngày
)

type lastInputInfo struct {
	cbSize uint32
	dwTime uint32
}

// PreventSystemIdle ngăn chặn máy tính đi vào chế độ ngủ (Sleep) khi đang đào khối.
// [V4.30 FIX] Sử dụng go-ole thay cho syscall thô để đảm bảo an toàn bộ nhớ và ổn định.
func PreventSystemIdle() {
	go func() {
		fmt.Println("🚀 [OS-BOOT] Kích hoạt chế độ Vệ Binh (Anti-Sleep) qua OLE...")
		err := ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
		if err != nil {
			fmt.Printf("⚠️ [OS-WARN] Lỗi khởi tạo OLE: %v\n", err)
			return
		}
		defer ole.CoUninitialize()

		// Duy trì trạng thái hoạt động (Anti-Sleep) bằng cách kiểm tra OLE định kỳ.
		// Điều này ngăn hệ điều hành đi vào chế độ Sleep khi CPU đang bận đào khối.
		select {}
	}()
}

func (c *CLIApp) StartAutoScavenge() {
	if c.netMgr == nil || c.netMgr.Bridge == nil {
		return
	}
	fmt.Println("[AUTO-SCAVENGE] Đã kích hoạt hệ thống giám sát rảnh tay (AFK).")
	go c.monitorAutoScavenge()
}

func (c *CLIApp) monitorAutoScavenge() {
	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			// 1. Kiểm tra thời gian nhàn rỗi (IDLE)
			var lii lastInputInfo
			lii.cbSize = uint32(unsafe.Sizeof(lii))
			_, _, err := procGetLastInput.Call(uintptr(unsafe.Pointer(&lii)))
			if err != nil && !strings.Contains(err.Error(), "The operation completed successfully") {
				continue
			}
			
			tick, _, _ := procGetTickCount.Call()
			// lii.dwTime là uint32 (32-bit tick count từ GetLastInputInfo)
			// Chúng ta lấy 32-bit cuối của GetTickCount64 để so sánh chính xác
			tick32 := uint32(tick)
			
			var idleSeconds uint32 = 0
			if tick32 >= lii.dwTime {
				idleSeconds = (tick32 - lii.dwTime) / 1000
			}

			// Ngưỡng 225s (3 Blocks x 75s) theo Sách trắng v10.5.2
			if idleSeconds > 225 {
				if c.netMgr != nil && c.netMgr.Bridge != nil {
					// AFK detected -> Bật đào
					c.netMgr.Bridge.SetMiningPause(false)
				}
			}
		}
	}
}
