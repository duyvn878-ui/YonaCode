//go:build !windows
// +build !windows

package user_interface

import (
	"fmt"
	"os"
	"time"
)

func (c *CLIApp) StartAutoScavenge() {
	fmt.Println("[AUTO-SCAVENGE] Chế độ Unix: Đang giám sát tải hệ thống (/proc/loadavg)...")
	
	go func() {
		for {
			data, err := os.ReadFile("/proc/loadavg")
			if err == nil {
				var load1 float64
				fmt.Sscanf(string(data), "%f", &load1)
				
				isBusy := load1 > 2.0 // Ngưỡng 2.0 load trung bình
				if c.netMgr != nil && c.netMgr.Bridge != nil {
					c.netMgr.Bridge.SetMiningPause(isBusy)
					if isBusy {
						fmt.Printf("\r[AUTO-SCAVENGE] ⚠️ CPU Load Cao (%.2f). Đã tạm dừng đào.   ", load1)
					}
				}
			}
			time.Sleep(10 * time.Second)
		}
	}()
}
