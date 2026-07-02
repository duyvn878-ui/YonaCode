package main

import (
	"fmt"
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
		color.Green("⛏️ " + i18n.T("mining_start"))
		fmt.Printf("   Address: %s | Threads: %d\n", addr, threads)

		if client == nil {
			color.Red("❌ Error: Không thể kết nối tới Node gRPC (Client nil)")
			return
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
}
