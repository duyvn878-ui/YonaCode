package main

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/fatih/color"
	"btc_genz/6_user_interface/i18n"
	pb_block "btc_genz/proto"
)

var miningCmd = &cobra.Command{
	Use:   "mine",
	Short: "⛏️ Khai thác (Mining) - Thu thập BTC_Z",
}

var miningStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Bắt đầu khai thác khối",
	Run: func(cmd *cobra.Command, args []string) {
		addr, _ := cmd.Flags().GetString("reward-address")
		threads, _ := cmd.Flags().GetInt("threads")
		device, _ := cmd.Flags().GetString("mining-device")
		
		color.Green("⛏️ " + i18n.T("mining_start"))
		fmt.Printf("   Address: %s | Threads: %d | Device: %s\n", addr, threads, device)

		if client == nil {
			color.Red("❌ Error: Không thể kết nối tới Node gRPC (Client nil)")
			return
		}

		// Đồng bộ thiết bị đào (mining-device) tới Node qua HTTP API
		if device != "" {
			device = strings.ToLower(strings.TrimSpace(device))
			if device == "cpu" || device == "gpu" || device == "hybrid" {
				parts := strings.Split(nodeAddr, ":")
				host := "127.0.0.1"
				grpcPort := 18080
				if len(parts) > 0 && parts[0] != "" {
					host = parts[0]
				}
				if len(parts) > 1 {
					fmt.Sscanf(parts[1], "%d", &grpcPort)
				}
				httpPort := grpcPort - 10000

				url := fmt.Sprintf("http://%s:%d/api/v1/node/mining-device", host, httpPort)
				payload := fmt.Sprintf(`{"device":"%s"}`, device)
				
				reqHttp, err := http.NewRequest("POST", url, strings.NewReader(payload))
				if err == nil {
					reqHttp.Header.Set("Content-Type", "application/json")
					tokenFile := filepath.Join(dbPath, ".auth_token")
					if data, err := os.ReadFile(tokenFile); err == nil {
						reqHttp.Header.Set("x-auth-token", strings.TrimSpace(string(data)))
					}

					clientHttp := &http.Client{Timeout: 5 * time.Second}
					respHttp, err := clientHttp.Do(reqHttp)
					if err == nil {
						respHttp.Body.Close()
						fmt.Printf("   [CONFIG] Đã đồng bộ thiết bị đào '%s' sang Node chính.\n", device)
					} else {
						fmt.Printf("   ⚠️ Không thể đồng bộ thiết bị đào sang Node: %v\n", err)
					}
				}
			}
		}

		_, err := client.StartMining(cmd.Context(), &pb_block.StartMiningRequest{
			MinerAddress: addr,
			Threads:      uint32(threads),
		})
		if err != nil {
			color.Red("❌ Error: %v", err)
			return
		}
		color.Cyan("🚀 Lệnh khai hỏa đã được gửi tới SCL Core thành công!")
	},
}

var miningStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Dừng khai thác",
	Run: func(cmd *cobra.Command, args []string) {
		color.Yellow("🛑 " + i18n.T("mining_stop"))
	},
}

var miningStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Hiển thị trạng thái khai thác cục bộ",
	Run: func(cmd *cobra.Command, args []string) {
		if client == nil {
			color.Red("❌ Error: Không thể kết nối tới Node RPC")
			return
		}
		resp, err := client.GetStatus(cmd.Context(), &pb_block.GetStatusRequest{})
		if err != nil {
			color.Red("❌ Error: %v", err)
			return
		}
		
		unit := "H/s"
		val := float64(resp.Hashrate)
		if val > 1_000_000 {
			val /= 1_000_000
			unit = "MH/s"
		} else if val > 1_000 {
			val /= 1_000
			unit = "KH/s"
		}
		
		statusStr := color.RedString("OFF")
		if resp.IsMining {
			statusStr = color.GreenString("ON")
		}

		fmt.Printf("[MINING] Status: %s | Local Hashrate: %.2f %s\n", statusStr, val, unit)
	},
}

func init() {
	rootCmd.AddCommand(miningCmd)
	miningCmd.AddCommand(miningStartCmd, miningStopCmd, miningStatusCmd)
	
	miningStartCmd.Flags().String("reward-address", "", "Địa chỉ nhận phần thưởng khối")
	miningStartCmd.Flags().Int("threads", 4, "Số luồng CPU sử dụng")
	miningStartCmd.Flags().String("mining-device", "cpu", "Thiết bị khai thác: cpu, gpu, hoặc hybrid")
}
