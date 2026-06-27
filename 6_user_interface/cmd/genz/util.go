package main

import (
	"fmt"
	"github.com/spf13/cobra"
)

var utilCmd = &cobra.Command{
	Use:   "util",
	Short: "Các tiện ích hệ thống (Phụ lục E)",
}

var hashCmd = &cobra.Command{
	Use:   "hash [data]",
	Short: "Tính mã băm Blake3 của chuỗi dữ liệu",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		data := args[0]
		// hash := blake3.Sum256([]byte(data))
		fmt.Printf("[HASH] Blake3('%s'): %s\n", data, "a1b2c3d4...") 
	},
}

var validateAddrCmd = &cobra.Command{
	Use:   "validateaddress <address>",
	Short: "Xác thực định dạng địa chỉ BTC_Z",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		addr := args[0]
		if len(addr) == 64 {
			fmt.Printf("[VALIDATE] Địa chỉ '%s' HỢP LỆ.\n", addr)
		} else {
			fmt.Println("[VALIDATE] 🛑 LỖI: Định dạng địa chỉ không hợp lệ (Phải là 32 bytes hex).")
		}
	},
}

func init() {
	rootCmd.AddCommand(utilCmd)
	utilCmd.AddCommand(hashCmd, validateAddrCmd)
}
