package node_p2p

import (
	"encoding/binary"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	pb_block "btc_genz/proto"

	"google.golang.org/protobuf/proto"
	"lukechampine.com/blake3"
)

/**
 * @file snapshot_manager.go
 * @brief Trình quản lý Snapshot tự động cho YonaCode.
 * @details Tự động tạo Snapshot cứ mỗi 1152 khối với độ trễ an toàn 1152 khối.
 * Kịch bản: Khi đạt khối 2304, tạo Snapshot của khối 1153.
 */

type SnapshotManager struct {
	dbPath    string
	bridge    BridgeInterface
	mu        sync.Mutex
	lastSnap  uint64
	isRunning bool // [VÁ LỖI TRÙNG LẶP] Cờ trạng thái đang tạo snapshot để tránh bắn trùng luồng
}


func NewSnapshotManager(dbPath string, bridge BridgeInterface) *SnapshotManager {
	snapDir := filepath.Join(dbPath, "snapshots")
	if _, err := os.Stat(snapDir); os.IsNotExist(err) {
		os.MkdirAll(snapDir, 0755)
	}

	// [AUTO-RECOVERY] Khôi phục lastSnap từ đĩa để tránh tạo lại các snapshot đã tồn tại
	var maxSnap uint64 = 0
	files, err := os.ReadDir(snapDir)
	if err == nil {
		for _, f := range files {
			if !f.IsDir() && filepath.Ext(f.Name()) == ".bin" && strings.HasPrefix(f.Name(), "snapshot_") {
				parts := strings.Split(f.Name(), "_")
				if len(parts) == 2 {
					hStr := strings.TrimSuffix(parts[1], ".bin")
					if h, err := strconv.ParseUint(hStr, 10, 64); err == nil {
						if h > maxSnap {
							maxSnap = h
						}
					}
				}
			}
		}
	}

	if maxSnap > 0 {
		log.Printf("[SNAPSHOT-RECOVERY] 📜 Đã khôi phục mốc snapshot mới nhất trên đĩa: #%d", maxSnap)
	}

	return &SnapshotManager{
		dbPath:   dbPath,
		bridge:   bridge,
		lastSnap: maxSnap,
	}
}

// ============================================================================
// LƯU Ý HỆ THỐNG: Tính năng Snapshot tự động đã được phát triển và kiểm toán hoàn thiện.
// Hiện tại, tính năng này tạm thời được tắt đi (qua cấu hình Master Switch) vì trong giai đoạn
// đầu, dung lượng mạng lưới còn rất nhẹ. Việc các Node mới thiết lập kết nối và tải toàn bộ
// dữ liệu tuần tự từ khối #0 (Full Sync) sẽ diễn ra nhanh hơn, an toàn hơn và giúp phân tán
// đầy đủ lịch sử sổ cái (Full Ledger History) giữa các thành viên.
// Cách kích hoạt lại: Chuyển EnableAutoSnapshot thành true.
// ============================================================================
const EnableAutoSnapshot = false

// OnBlockCommitted: Callback được gọi mỗi khi có khối mới chốt hạ.
func (sm *SnapshotManager) OnBlockCommitted(height uint64) {
	// 🛡️ [MASTER SWITCH] Vô hiệu hóa tạo Snapshot tự động trong giai đoạn mạng lưới còn nhẹ.
	if !EnableAutoSnapshot {
		return
	}

	// Chỉ bắt đầu xử lý khi đã vượt qua 2 Epoch (Grace Period) để khớp với Đại Thanh Trừng
	if height < 2304 {
		return
	}

	sm.mu.Lock()
	if sm.isRunning {
		sm.mu.Unlock()
		return // Nếu đang có tiến trình tạo snapshot chạy nền, bỏ qua để tránh trùng lặp tài nguyên
	}
	lastSnap := sm.lastSnap
	sm.mu.Unlock()

	// Lấy mốc an toàn (oldestHeight) từ Rust Core để tránh tạo snapshot cho các mốc đã bị Pruned
	oldestHeight := sm.bridge.GetOldestHeight()

	// [VANGUARD-AUTO-BOOTSTRAP] Tự động tạo snapshot khi không có file nào trên đĩa.
	// Tại sao: Khi node mới khởi chạy hoặc thư mục snapshots/ bị mất/trống,
	// node sẽ không có snapshot để phục vụ FastSync cho các peer khác.
	// Logic này chủ động tìm mốc Epoch gần nhất mà dữ liệu còn tồn tại trên DB
	// và tạo snapshot ngay lập tức để node luôn sẵn sàng phục vụ P2P.
	if lastSnap == 0 && height >= 2*1152 {
		bestTarget := sm.findBestBootstrapTarget(height, oldestHeight)
		if bestTarget > 0 {
			log.Printf("[SNAPSHOT-BOOTSTRAP] 🚀 Không tìm thấy snapshot nào trên đĩa! Tự động khởi tạo snapshot tại mốc Epoch gần nhất: #%d", bestTarget)
			sm.mu.Lock()
			sm.isRunning = true
			sm.mu.Unlock()

			go func() {
				defer func() {
					sm.mu.Lock()
					sm.isRunning = false
					sm.mu.Unlock()
				}()
				sm.CreateSnapshot(bestTarget)
			}()
			return // Thoát sớm, các mốc cũ hơn sẽ được bù lại ở lần gọi tiếp theo
		}
	}

	// [VANGUARD-SNAPSHOT-FIX] Khắc phục lỗi bỏ sót tạo snapshot khi nhảy vọt chiều cao trong lúc sync
	// Duyệt tìm tất cả các mốc kỉ nguyên (Epoch boundaries) đã vượt qua nhưng chưa được tạo snapshot.
	// Công thức: k * 1152 <= height => k <= height / 1152
	maxK := height / 1152
	var targets []uint64
	for k := uint64(2); k <= maxK; k++ {
		targetHeight := (k-1)*1152 + 1
		if targetHeight >= oldestHeight && targetHeight > lastSnap {
			targets = append(targets, targetHeight)
		}
	}

	// Thực hiện tạo các snapshot tuần tự trong một goroutine nền để tránh deadlock/blocking vòng lặp chính
	if len(targets) > 0 {
		sm.mu.Lock()
		sm.isRunning = true
		sm.mu.Unlock()

		go func() {
			defer func() {
				sm.mu.Lock()
				sm.isRunning = false
				sm.mu.Unlock()
			}()
			for _, target := range targets {
				sm.CreateSnapshot(target)
			}
		}()
	}
}

// findBestBootstrapTarget: Tìm mốc Epoch boundary tốt nhất để tạo snapshot bootstrap.
// Tại sao duyệt ngược: Ưu tiên mốc Epoch MỚI NHẤT (gần đỉnh chuỗi nhất) mà dữ liệu
// vẫn còn tồn tại trong DB (chưa bị Purge). Snapshot ở mốc cao nhất sẽ hữu ích nhất
// cho các peer đang FastSync vì họ cần trạng thái gần đỉnh chuỗi nhất.
func (sm *SnapshotManager) findBestBootstrapTarget(currentHeight uint64, oldestHeight uint64) uint64 {
	maxK := currentHeight / 1152
	// Duyệt ngược từ epoch gần nhất về quá khứ, tìm mốc đầu tiên mà dữ liệu còn khả dụng
	for k := maxK; k >= 2; k-- {
		targetHeight := (k - 1) * 1152 + 1
		// Mốc này phải nằm trong vùng dữ liệu chưa bị Purge
		if targetHeight >= oldestHeight && targetHeight < currentHeight {
			// Kiểm tra xem Rust Core có thực sự nắm giữ Block Hash tại mốc này không
			hash := sm.bridge.GetBlockHash(targetHeight)
			if len(hash) == 32 {
				return targetHeight
			}
		}
	}
	return 0
}

func (sm *SnapshotManager) CreateSnapshot(height uint64) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if height <= sm.lastSnap {
		return
	}

	startTime := time.Now()
	log.Printf("[SNAPSHOT-AUTO] 📸 Bắt đầu tạo Snapshot tự động cho mỏ neo #%d...", height)

	// Gọi Rust Core trích xuất dữ liệu lịch sử
	data := sm.bridge.ExportStateSnapshotAtHeightRaw(height)
	if len(data) == 0 {
		log.Printf("[SNAPSHOT-ERROR] ❌ Không thể trích xuất dữ liệu tại #%d", height)
		return
	}

	// Lưu file
	fileName := fmt.Sprintf("snapshot_%d.bin", height)
	filePath := filepath.Join(sm.dbPath, "snapshots", fileName)
	
	err := os.WriteFile(filePath, data, 0644)
	if err != nil {
		log.Printf("[SNAPSHOT-ERROR] ❌ Lỗi ghi file snapshot: %v", err)
		return
	}

	// Lấy StateRoot từ Block Header để làm neo chứng thực trong Manifest
	var expectedRoot []byte = make([]byte, 32)
	hash := sm.bridge.GetBlockHash(height)
	if len(hash) == 32 {
		headerRaw := sm.bridge.GetHeaderRaw(hash)
		if len(headerRaw) > 0 {
			var hdr pb_block.BlockHeader
			if err := proto.Unmarshal(headerRaw, &hdr); err == nil && hdr.StateRoot != nil {
				expectedRoot = hdr.StateRoot.Value
			}
		}
	}

	// Sinh Manifest (Danh sách hash Blake3 của từng mảnh 2MB)
	chunkSize := 2 * 1024 * 1024 // 2MB
	var hashes []byte
	numChunks := uint32(0)
	for i := 0; i < len(data); i += chunkSize {
		end := i + chunkSize
		if end > len(data) {
			end = len(data)
		}
		chunk := data[i:end]
		h := blake3.Sum256(chunk)
		hashes = append(hashes, h[:]...)
		numChunks++
	}

	manifestBytes := make([]byte, 32+4+len(hashes))
	copy(manifestBytes[0:32], expectedRoot)
	binary.LittleEndian.PutUint32(manifestBytes[32:36], numChunks)
	copy(manifestBytes[36:], hashes)

	manifestName := fmt.Sprintf("snapshot_%d.manifest", height)
	manifestPath := filepath.Join(sm.dbPath, "snapshots", manifestName)
	if err := os.WriteFile(manifestPath, manifestBytes, 0644); err != nil {
		log.Printf("[SNAPSHOT-ERROR] ❌ Lỗi ghi file manifest: %v", err)
	} else {
		log.Printf("[SNAPSHOT-SUCCESS] 📜 Đã sinh tệp Manifest thành công: %s (%d chunks)", manifestName, numChunks)
	}

	sm.lastSnap = height
	log.Printf("[SNAPSHOT-SUCCESS] ✅ Đã lưu Snapshot #%d thành công! (Size: %d bytes, Time: %v)", 
		height, len(data), time.Since(startTime))
	
	// Dọn dẹp các snapshot quá cũ (Chỉ giữ lại 2 bản gần nhất để tiết kiệm dung lượng)
	sm.CleanupOldSnapshots()
}

func (sm *SnapshotManager) CleanupOldSnapshots() {
	snapDir := filepath.Join(sm.dbPath, "snapshots")
	files, err := os.ReadDir(snapDir)
	if err != nil { return }

	// Tạo struct để chứa tên file và số height tương ứng
	type snapFile struct {
		Name   string
		Height uint64
	}

	var snapFiles []snapFile
	for _, f := range files {
		if !f.IsDir() && filepath.Ext(f.Name()) == ".bin" && strings.HasPrefix(f.Name(), "snapshot_") {
			name := f.Name()
			parts := strings.Split(name, "_")
			if len(parts) == 2 {
				hStr := strings.TrimSuffix(parts[1], ".bin")
				if h, err := strconv.ParseUint(hStr, 10, 64); err == nil {
					snapFiles = append(snapFiles, snapFile{Name: name, Height: h})
				}
			}
		}
	}

	if len(snapFiles) > 2 {
		log.Printf("[SNAPSHOT-CLEANUP] 🧹 Phát hiện %d bản sao lưu. Đang dọn dẹp để giữ lại 2 bản mới nhất...", len(snapFiles))
		
		// Sắp xếp theo SỐ HỌC (Numeric), chiều cao nhỏ (cũ) xếp lên đầu
		sort.Slice(snapFiles, func(i, j int) bool {
			return snapFiles[i].Height < snapFiles[j].Height
		})

		// Xóa các file cũ nhất
		for i := 0; i < len(snapFiles)-2; i++ {
			toDelete := filepath.Join(snapDir, snapFiles[i].Name)
			os.Remove(toDelete)
			
			// Xóa kèm file manifest tương ứng
			manifestToDelete := filepath.Join(snapDir, fmt.Sprintf("snapshot_%d.manifest", snapFiles[i].Height))
			os.Remove(manifestToDelete)
			
			log.Printf("[SNAPSHOT-CLEANUP] 🗑️  Đã xóa snapshot và manifest cũ cho height #%d", snapFiles[i].Height)
		}
	}
}
