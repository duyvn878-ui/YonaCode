package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"github.com/fatih/color"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"btc_genz/6_user_interface/i18n"
	pb_block "btc_genz/proto"
)

var (
	nodeAddr   string
	jsonOutput bool
	lang       string
	dbPath     string
	conn       *grpc.ClientConn
	client     pb_block.BlockchainServiceClient
	isDoubleClicked bool
)

var rootCmd = &cobra.Command{
	Use:   "yonacode",
	Short: "🚀 YonaCode Go - Minimalist, Immutable, Ultralight",
	Long: fmt.Sprintf(`%s
 %s
 %s
 %s
 %s
 %s
 
   Y O N A C O D E   G O   ( v 1 . 0 )
 -----------------------------------------------
 Tối Giản - Bất Biến - Siêu Nhẹ (Minimalist - Immutable - Ultralight)`,
		color.CyanString(" __   __              _   _        ____          _         ____       "),
		color.CyanString(" \\ \\ / /__  _ __   __ _| \\ | |      / ___|___   __| | ___   / ___| ___  "),
		color.CyanString("  \\ V / _ \\| '_ \\ / _` |  \\| |     | |   / _ \\ / _` |/ _ \\ | |  _ / _ \\ "),
		color.CyanString("   | | (_) | | | | (_| | |\\  |     | |___ (_) | (_| |  __/ | |_| | (_) |"),
		color.CyanString("   |_|\\___/|_| |_|\\__,_|_| \\_|      \\____\\___/ \\__,_|\\___|  \\____|\\___/ "),
		color.CyanString("                                                                        ")),
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		i18n.SetLang(lang)

		// [SECURITY-HARDENING] Đọc token từ file tạm .auth_token để đính kèm gRPC calls
		var token string
		tokenFile := filepath.Join(dbPath, ".auth_token")
		if data, err := os.ReadFile(tokenFile); err == nil {
			token = strings.TrimSpace(string(data))
		}

		dialOpts := []grpc.DialOption{
			grpc.WithInsecure(),
		}
		if token != "" {
			dialOpts = append(dialOpts, grpc.WithUnaryInterceptor(func(ctx context.Context, method string, req, reply interface{}, cc *grpc.ClientConn, invoker grpc.UnaryInvoker, opts ...grpc.CallOption) error {
				ctx = metadata.AppendToOutgoingContext(ctx, "x-auth-token", token)
				return invoker(ctx, method, req, reply, cc, opts...)
			}))
		}

		var err error
		conn, err = grpc.Dial(nodeAddr, dialOpts...)
		if err == nil {
			client = pb_block.NewBlockchainServiceClient(conn)
		}
	},
}

func findProjectRoot() (string, error) {
	currDir, err := os.Getwd()
	if err != nil {
		return "", err
	}

	for {
		if _, err := os.Stat(filepath.Join(currDir, "go.mod")); err == nil {
			return currDir, nil
		}
		parent := filepath.Dir(currDir)
		if parent == currDir {
			break
		}
		currDir = parent
	}
	return "", fmt.Errorf("Project root (go.mod) not found")
}

func Execute() {
	// [VANGUARD-LOGGING] Mặc định ghi log ra Console để bảo vệ SSD (SSD Wear Prevention)
	log.SetOutput(os.Stderr)
	log.SetFlags(log.Ldate | log.Ltime | log.Lmicroseconds)



	if len(os.Args) == 1 {
		isDoubleClicked = true
		// [TỰ ĐỘNG CHẠY PHÙ HỢP NGƯỜI DÙNG] Khi người dùng nhấp đúp (không tham số), 
		// tự động khởi chạy: 'yonacode node start' với cấu hình mặc định (tắt đào để bảo vệ tính an toàn đồng thuận).
		fmt.Println("🚀 Không phát hiện tham số. Tự động khởi chạy: 'yonacode node start' với cấu hình mặc định...")
		os.Args = []string{os.Args[0], "node", "start"}
	}

	// Bắt mọi Panic sập hệ thống (Crash ngầm)
	defer func() {
		if r := recover(); r != nil {
			fmt.Printf("\n[LỖI SẬP NGUỒN (PANIC)] Hệ thống đã sập vì lỗi Code:\n%v\n", r)
			if isDoubleClicked {
				fmt.Println("Nhấn Enter để thoát...")
				fmt.Scanln()
			}
		}
	}()

	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if isDoubleClicked {
			fmt.Println("\n[LỖI] Hệ thống không thể khởi động. Nhấn Enter để thoát...")
			fmt.Scanln()
		}
		os.Exit(1)
	}

	if isDoubleClicked {
		fmt.Println("\n[THÔNG BÁO] Node đã dừng hoạt động. Nhấn Enter để kết thúc...")
		fmt.Scanln()
	}
	if conn != nil { conn.Close() }
}

func init() {
	cobra.MousetrapHelpText = "" // [QUAN TRỌNG] Vô hiệu hóa cảnh báo Mousetrap của Cobra
	
	rootCmd.PersistentFlags().StringVar(&nodeAddr, "node-addr", "localhost:18080", "Địa chỉ gRPC của Node (Node RPC Address)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Xuất kết quả dưới dạng JSON (JSON Output)")
	rootCmd.PersistentFlags().StringVar(&lang, "lang", "vnm", "Ngôn ngữ / Language (vnm/eng)")
	rootCmd.PersistentFlags().StringVar(&dbPath, "db-path", "node", "Đường dẫn thư mục Database của Node")
}
