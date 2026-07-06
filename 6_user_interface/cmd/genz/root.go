package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
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

type GlobalConfig struct {
	InstallDir string `json:"install_dir"`
	DbPath     string `json:"db_path"`
}

func getGlobalConfigPath() string {
	var baseDir string
	if runtime.GOOS == "windows" {
		baseDir = os.Getenv("APPDATA")
		if baseDir == "" {
			baseDir = os.Getenv("USERPROFILE")
		}
	} else {
		baseDir = os.Getenv("HOME")
	}
	if baseDir == "" {
		baseDir = "."
	}
	return filepath.Join(baseDir, ".yonacode", "config.json")
}

func readGlobalConfig() (*GlobalConfig, error) {
	cfgPath := getGlobalConfigPath()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		return nil, err
	}
	var cfg GlobalConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func writeGlobalConfig(cfg *GlobalConfig) error {
	cfgPath := getGlobalConfigPath()
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cfgPath, data, 0644)
}

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

		// [VANGUARD-PREPACKAGED-LEDGER] Tự động phát hiện dữ liệu sổ cái đi kèm (node/scl hoặc data/scl)
		execPath, err := os.Executable()
		if err == nil {
			currentDir := filepath.Dir(execPath)
			nodeSclPath := filepath.Join(currentDir, "node", "scl")
			dataSclPath := filepath.Join(currentDir, "data", "scl")

			var selectedDbPath string
			if info, err := os.Stat(nodeSclPath); err == nil && info.IsDir() {
				selectedDbPath = filepath.Join(currentDir, "node")
			} else if info, err := os.Stat(dataSclPath); err == nil && info.IsDir() {
				selectedDbPath = filepath.Join(currentDir, "data")
			}

			if selectedDbPath != "" {
				color.Green("✅ Phát hiện dữ liệu sổ cái đóng gói sẵn tại: %s", selectedDbPath)
				color.Green("🚀 Tự động khởi chạy Node YonaCode mà không cần cấu hình thủ công...")
				cfg := &GlobalConfig{
					InstallDir: currentDir,
					DbPath:     selectedDbPath,
				}
				writeGlobalConfig(cfg)
				os.Args = []string{os.Args[0], "node", "start", "--db-path", selectedDbPath}
				goto skipWizard
			}
		}

		{
			cfg, err := readGlobalConfig()
			if err == nil && cfg != nil && cfg.InstallDir != "" && cfg.DbPath != "" {
				// Đã cài đặt, kiểm tra vị trí file chạy
				execPath, err := os.Executable()
				if err == nil {
					currentDir := filepath.Dir(execPath)
					targetDir := cfg.InstallDir
					cleanCurr, err1 := filepath.EvalSymlinks(currentDir)
					cleanTarget, err2 := filepath.EvalSymlinks(targetDir)
					if err1 == nil && err2 == nil && cleanCurr != cleanTarget {
						// Người dùng chạy file exe ở thư mục khác (Downloads) -> Tự động cập nhật
						color.Cyan("\n🔄 Phát hiện bản cập nhật mới! Đang tự động cập nhật hệ thống...")
						filesToCopy := []string{
							"YonaCode", "YonaCode.exe",
							"btc_genz_scl.dll", "libbtc_genz_scl.dylib", "scl_server",
							"genz_miner", "genz_miner.exe",
							"cli_yona_code", "cli_yona_code.exe",
						}

						copiedCount := 0
						for _, fileName := range filesToCopy {
							srcFile := filepath.Join(currentDir, fileName)
							if _, err := os.Stat(srcFile); err == nil {
								destFile := filepath.Join(targetDir, fileName)
								if fileName == "YonaCode" || fileName == "YonaCode.exe" {
									oldBackup := destFile + ".old"
									os.Remove(oldBackup)
									os.Rename(destFile, oldBackup)
								}
								errCopy := copyFile(srcFile, destFile)
								if errCopy == nil {
									copiedCount++
								}
							}
						}
						color.Green("✅ Đã cập nhật thành công %d tệp vào thư mục cài đặt gốc: %s", copiedCount, targetDir)
						
						// Chuyển hướng chạy exe ở thư mục gốc
						binaryName := "YonaCode"
						if runtime.GOOS == "windows" {
							binaryName = "YonaCode.exe"
						}
						execBinary := filepath.Join(targetDir, binaryName)
						cmd := exec.Command(execBinary)
						cmd.Dir = targetDir
						cmd.Stdout = os.Stdout
						cmd.Stderr = os.Stderr
						if errStart := cmd.Start(); errStart == nil {
							color.Green("🚀 Đang chuyển hướng khởi chạy Node ở thư mục cài đặt gốc...")
							os.Exit(0)
						} else {
							color.Red("❌ Lỗi khởi chạy tiến trình cập nhật: %v", errStart)
						}
					} else {
						// Chạy trực tiếp node
						color.Green("🚀 Tự động khởi chạy Node YonaCode với dữ liệu: %s", cfg.DbPath)
						os.Args = []string{os.Args[0], "node", "start", "--db-path", cfg.DbPath}
					}
				}
			} else {
				// Lần đầu chạy, hiển thị Installer Wizard
				runInstallationWizard()
			}
		}

	skipWizard:
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

func runInstallationWizard() {
	color.Cyan("======================================================")
	color.Cyan("🚀 TRÌNH CÀI ĐẶT & CẬP NHẬT TỰ ĐỘNG (YONACODE INSTALLER)")
	color.Cyan("======================================================")
	fmt.Println("\nVui lòng chọn chế độ hoạt động:")
	color.Green("  [1] CẬP NHẬT PHIÊN BẢN (Update existing installation)")
	fmt.Println("      - Tự động sao chép các tệp nhị phân mới đè vào thư mục cũ.")
	fmt.Println("      - Giữ nguyên toàn bộ cơ sở dữ liệu blockchain và ví của bạn.")
	color.Green("  [2] CÀI ĐẶT MỚI (Fresh Installation)")
	fmt.Println("      - Khởi tạo thư mục dữ liệu mới và chạy node tại đây.")
	color.Green("  [3] CHẠY TRỰC TIẾP (Run directly)")
	fmt.Println("      - Khởi chạy node ngay lập tức tại thư mục hiện tại.")

	fmt.Print("\nNhập lựa chọn của bạn (1/2/3) [Mặc định: 3]: ")
	var choice string
	fmt.Scanln(&choice)
	choice = strings.TrimSpace(choice)
	if choice == "" {
		choice = "3"
	}

	switch choice {
	case "1":
		handleUpdateWizard()
	case "2":
		handleFreshInstallWizard()
	default:
		fmt.Println("🚀 Đang khởi chạy node trực tiếp...")
		os.Args = []string{os.Args[0], "node", "start"}
	}
}

func handleUpdateWizard() {
	color.Cyan("\n--- CẬP NHẬT PHIÊN BẢN ---")
	execPath, err := os.Executable()
	if err != nil {
		color.Red("❌ Không thể xác định đường dẫn tệp chạy hiện tại: %v", err)
		return
	}
	currentDir := filepath.Dir(execPath)

	fmt.Println("Vui lòng nhập đường dẫn thư mục cài đặt YonaCode cũ của bạn")
	fmt.Printf("(Ví dụ: D:\\hanhtrinhhocta-p\\sssd\\BTC hoặc đường dẫn chứa thư mục data/):\n")
	fmt.Print("👉 Đường dẫn cũ: ")

	var targetDir string
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err == nil {
		targetDir = strings.TrimSpace(line)
	}

	if targetDir == "" {
		color.Red("❌ Đường dẫn không được để trống!")
		waitForExit()
		os.Exit(1)
	}

	if _, err := os.Stat(targetDir); os.IsNotExist(err) {
		color.Red("❌ Thư mục đích không tồn tại: %s", targetDir)
		waitForExit()
		os.Exit(1)
	}

	color.Yellow("\nĐang sao chép các tệp tin cập nhật mới...")
	filesToCopy := []string{
		"YonaCode", "YonaCode.exe",
		"btc_genz_scl.dll", "libbtc_genz_scl.dylib", "scl_server",
		"genz_miner", "genz_miner.exe",
		"cli_yona_code", "cli_yona_code.exe",
	}

	copiedCount := 0
	for _, fileName := range filesToCopy {
		srcFile := filepath.Join(currentDir, fileName)
		if _, err := os.Stat(srcFile); err == nil {
			destFile := filepath.Join(targetDir, fileName)
			err := copyFile(srcFile, destFile)
			if err != nil {
				color.Red("⚠️ Không thể sao chép %s: %v", fileName, err)
			} else {
				fmt.Printf("✅ Đã cập nhật: %s\n", fileName)
				copiedCount++
			}
		}
	}

	if copiedCount == 0 {
		color.Red("❌ Không tìm thấy tệp nhị phân nào để cập nhật trong thư mục hiện tại!")
		waitForExit()
		os.Exit(1)
	}

	color.Green("\n🎉 CẬP NHẬT THÀNH CÔNG %d TỆP NHỊ PHÂN!", copiedCount)
	fmt.Printf("Thư mục đã được cập nhật: %s\n", targetDir)

	fmt.Print("\nBạn có muốn khởi chạy Node tại thư mục cũ ngay lập tức? (y/n) [Mặc định: y]: ")
	var runChoice string
	fmt.Scanln(&runChoice)
	runChoice = strings.ToLower(strings.TrimSpace(runChoice))
	if runChoice == "" || runChoice == "y" || runChoice == "yes" {
		color.Green("🚀 Đang khởi chạy Node tại thư mục cũ...")
		binaryName := "YonaCode"
		if runtime.GOOS == "windows" {
			binaryName = "YonaCode.exe"
		}
		execBinary := filepath.Join(targetDir, binaryName)

		cmd := exec.Command(execBinary, "node", "start")
		cmd.Dir = targetDir
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr

		err := cmd.Start()
		if err != nil {
			color.Red("❌ Lỗi khởi chạy tiến trình mới: %v", err)
		} else {
			color.Green("✅ Tiến trình mới đã bắt đầu hoạt động.")
			os.Exit(0)
		}
	} else {
		fmt.Println("Hoàn tất. Bạn có thể tự khởi chạy node tại thư mục cũ.")
		waitForExit()
		os.Exit(0)
	}
}

func handleFreshInstallWizard() {
	color.Cyan("\n--- CÀI ĐẶT MỚI ---")

	// Dò quét ổ đĩa khả dụng
	var drives []string
	if runtime.GOOS == "windows" {
		for _, d := range []string{"C", "D", "E", "F", "G", "H", "I", "J"} {
			path := d + ":\\"
			if _, err := os.Stat(path); err == nil {
				drives = append(drives, path)
			}
		}
	} else {
		// Trên Unix/Linux/macOS
		drives = []string{"/", "/home", "/mnt", "/media"}
	}

	fmt.Println("\nPhát hiện các ổ đĩa khả dụng trên hệ thống:")
	for i, d := range drives {
		fmt.Printf("  [%d] %s\n", i+1, d)
	}

	fmt.Printf("\nChọn ổ đĩa bạn muốn lưu trữ dữ liệu (1-%d) [Mặc định: 1]: ", len(drives))
	var driveChoice string
	fmt.Scanln(&driveChoice)
	driveChoice = strings.TrimSpace(driveChoice)

	selectedIndex := 0
	if driveChoice != "" {
		var idx int
		_, err := fmt.Sscanf(driveChoice, "%d", &idx)
		if err == nil && idx >= 1 && idx <= len(drives) {
			selectedIndex = idx - 1
		}
	}
	selectedDrive := drives[selectedIndex]
	color.Green("-> Đã chọn ổ đĩa: %s", selectedDrive)

	// Gợi ý thư mục mặc định trên ổ đĩa đã chọn
	suggestedDir := filepath.Join(selectedDrive, "yonacode")
	if runtime.GOOS != "windows" {
		if selectedDrive == "/" {
			suggestedDir = "/root/yonacode"
		} else {
			suggestedDir = filepath.Join(selectedDrive, "yonacode")
		}
	}

	fmt.Printf("\nĐường dẫn gợi ý: %s\n", suggestedDir)
	fmt.Println("Nhấn Enter để đồng ý hoặc nhập đường dẫn con mới mong muốn")
	fmt.Print("👉 Đường dẫn cài đặt: ")

	var installDir string
	reader := bufio.NewReader(os.Stdin)
	line, err := reader.ReadString('\n')
	if err == nil {
		installDir = strings.TrimSpace(line)
	}

	if installDir == "" {
		installDir = suggestedDir
	}

	absPath, err := filepath.Abs(installDir)
	if err == nil {
		installDir = absPath
	}

	if err := os.MkdirAll(installDir, 0755); err != nil {
		color.Red("❌ Không thể tạo thư mục cài đặt: %v", err)
		waitForExit()
		os.Exit(1)
	}

	execPath, _ := os.Executable()
	currentDir := filepath.Dir(execPath)
	filesToCopy := []string{
		"YonaCode", "YonaCode.exe",
		"btc_genz_scl.dll", "libbtc_genz_scl.dylib", "scl_server",
		"genz_miner", "genz_miner.exe",
		"cli_yona_code", "cli_yona_code.exe",
	}

	copiedCount := 0
	for _, fileName := range filesToCopy {
		srcFile := filepath.Join(currentDir, fileName)
		if _, err := os.Stat(srcFile); err == nil {
			destFile := filepath.Join(installDir, fileName)
			err := copyFile(srcFile, destFile)
			if err == nil {
				copiedCount++
			}
		}
	}

	color.Green("✅ Đã khởi tạo cấu hình cài đặt mới tại: %s (Sao chép %d tệp)", installDir, copiedCount)

	// [VANGUARD-OPTIMIZATION] Tự động sao chép dữ liệu sổ cái đóng gói sẵn (scl) nếu phát hiện
	dbPathTarget := filepath.Join(installDir, "data")
	var srcDataDir string
	if info, err := os.Stat(filepath.Join(currentDir, "node")); err == nil && info.IsDir() {
		srcDataDir = filepath.Join(currentDir, "node")
	} else if info, err := os.Stat(filepath.Join(currentDir, "data")); err == nil && info.IsDir() {
		srcDataDir = filepath.Join(currentDir, "data")
	}

	if srcDataDir != "" {
		color.Yellow("\n📦 Phát hiện dữ liệu sổ cái đóng gói sẵn tại: %s", filepath.Base(srcDataDir))
		color.Yellow("⏳ Đang tự động tích hợp dữ liệu (scl/ledger) sang thư mục cài đặt gốc...")
		if errCopy := copyDir(srcDataDir, dbPathTarget); errCopy != nil {
			color.Red("⚠️ Lỗi chuyển giao dữ liệu sổ cái: %v", errCopy)
		} else {
			color.Green("✅ Đã tích hợp dữ liệu sổ cái đóng gói sẵn thành công!")
		}
	}

	// Lưu cấu hình toàn cục
	cfg := &GlobalConfig{
		InstallDir: installDir,
		DbPath:     dbPathTarget,
	}
	if err := writeGlobalConfig(cfg); err != nil {
		color.Red("⚠️ Không thể lưu cấu hình toàn cục: %v", err)
	} else {
		color.Green("💾 Đã lưu cấu hình cài đặt toàn cục thành công!")
	}

	os.Args = []string{os.Args[0], "node", "start", "--db-path", cfg.DbPath}
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, in)
	if err != nil {
		return err
	}
	return os.Chmod(dst, 0755)
}

func copyDir(src string, dst string) error {
	srcInfo, err := os.Stat(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dst, srcInfo.Mode()); err != nil {
		return err
	}
	entries, err := os.ReadDir(src)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		srcPath := filepath.Join(src, entry.Name())
		dstPath := filepath.Join(dst, entry.Name())
		if entry.IsDir() {
			if err := copyDir(srcPath, dstPath); err != nil {
				return err
			}
		} else {
			if err := copyFile(srcPath, dstPath); err != nil {
				return err
			}
		}
	}
	return nil
}

func waitForExit() {
	if isDoubleClicked {
		fmt.Println("\nNhấn Enter để kết thúc...")
		fmt.Scanln()
	}
}

func init() {
	cobra.MousetrapHelpText = "" // [QUAN TRỌNG] Vô hiệu hóa cảnh báo Mousetrap của Cobra
	
	rootCmd.PersistentFlags().StringVar(&nodeAddr, "node-addr", "localhost:18080", "Địa chỉ gRPC của Node (Node RPC Address)")
	rootCmd.PersistentFlags().BoolVar(&jsonOutput, "json", false, "Xuất kết quả dưới dạng JSON (JSON Output)")
	rootCmd.PersistentFlags().StringVar(&lang, "lang", "vnm", "Ngôn ngữ / Language (vnm/eng)")
	rootCmd.PersistentFlags().StringVar(&dbPath, "db-path", "node", "Đường dẫn thư mục Database của Node")
}
