package go_bridge

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"time"

	pb "btc_genz/proto"

	"github.com/near/borsh-go"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/keepalive"
	"google.golang.org/grpc/metadata"
)

type SclClient struct {
	conn      *grpc.ClientConn
	client    pb.SclServiceClient
	authToken string // [SECURITY-HARDENING] Shared Secret Token cho destructive APIs
}

// [SECURITY-HARDENING] SetAuthToken cập nhật Shared Secret Token
func (c *SclClient) SetAuthToken(token string) {
	c.authToken = token
	log.Printf("[GRPC-CLIENT] 🔑 Token Xác Thực đã được nạp thành công (%d ký tự)", len(token))
}

// [SECURITY-HARDENING] authCtx tạo context với auth token trong metadata
// [VANGUARD-FIX] Bổ sung client-id và timestamp để vượt qua kiểm tra bảo mật nghiêm ngặt của Rust Core
func (c *SclClient) authCtx(parent context.Context) context.Context {
	md := metadata.Pairs(
		"x-auth-token", c.authToken,
		"client-id", "yonacode-go-bridge",
		"timestamp", fmt.Sprintf("%d", time.Now().Unix()),
	)
	return metadata.NewOutgoingContext(parent, md)
}

func NewSclClient(addr string) (*SclClient, error) {
	// [gRPC-KEEP-ALIVE] Cấu hình Ping định kỳ để giữ kết nối luôn nóng, chống Firewall/OS ngắt kết nối Idle
	kacp := keepalive.ClientParameters{
		Time:                10 * time.Second, // Mỗi 10s ping một phát để giữ kết nối luôn ấm
		Timeout:             time.Second,      // Đợi ping phản hồi trong tối đa 1s
		PermitWithoutStream: true,             // Cho phép ping ngay cả khi không có luồng dữ liệu active
	}

	opts := []grpc.DialOption{
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithKeepaliveParams(kacp),
		// [BUGFIX-WITHBLOCK] Loại bỏ WithBlock để kết nối được thiết lập dưới nền mà không chặn luồng chính vô hạn
		grpc.WithDefaultCallOptions(
			grpc.WaitForReady(true),
			grpc.MaxCallRecvMsgSize(512*1024*1024), // Tại sao: Khôi phục lại 512MB để đảm bảo quy trình Reorg/Sync khối lượng lớn không bị lỗi gãy kết nối
			grpc.MaxCallSendMsgSize(512*1024*1024),
		), // [V1.5 FIX] Hỗ trợ khối khổng lồ 35MB+ và Batch khối nặng
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second) // Tăng timeout dial lên 10s
	defer cancel()

	conn, err := grpc.DialContext(ctx, addr, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to connect via TCP %s: %v", addr, err)
	}

	return &SclClient{
		conn:   conn,
		client: pb.NewSclServiceClient(conn),
	}, nil
}

func (c *SclClient) Close() {
	c.conn.Close()
}

func (c *SclClient) InitScl(path string) error {
	// TẠI SAO CẦN TĂNG TIMEOUT LÊN 180 GIÂY?
	// Khi database RocksDB có dung lượng lớn (trên 1GB) chứa hơn 50.000 khối và nhiều ví,
	// tiến trình SCL Core (Rust) cần nhiều thời gian ở giai đoạn khởi động để mở RocksDB,
	// load cây trạng thái JMT, và quét lại lịch sử giao dịch/thợ đào. Timeout cũ 20 giây là quá ngắn,
	// dễ gây lỗi "context deadline exceeded" (timeout gRPC). Khi đó Go Node vẫn tiếp tục chạy
	// nhưng lõi Rust chưa được khởi tạo, làm cho node bị kẹt cứng chiều cao và không thể đồng bộ tiếp.
	// [MAINNET-TIMEOUT] Tăng timeout lên 300 giây (5 phút) để dành đủ thời gian cho RocksDB
	// tự động khôi phục và xử lý Write-Ahead Log (WAL) sau khi tắt đột ngột trên cơ sở dữ liệu lớn 50GB+.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	authCtx := c.authCtx(ctx)
	_, err := c.client.InitScl(authCtx, &pb.InitRequest{DbPath: path})
	if err != nil {
		log.Printf("[SclClient] ❌ InitScl RPC failed: %v", err)
	}
	return err
}

func (c *SclClient) GetAccountState(addr []byte) *pb.AccountSnapshot {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetAccountState(ctx, &pb.BalanceRequest{Address: addr})
	if err != nil || resp == nil {
		return nil
	}
	return resp
}

func (c *SclClient) GetBalance(addr []byte) uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetBalance(ctx, &pb.BalanceRequest{Address: addr})
	if err != nil || resp == nil {
		return 0
	}
	return resp.Balance
}

func (c *SclClient) GetNonce(addr []byte) uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetNonce(ctx, &pb.NonceRequest{Address: addr})
	if err != nil || resp == nil {
		return 0
	}
	return resp.Nonce
}

func (c *SclClient) GetStateRoot() []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetStateRoot(ctx, &pb.Empty{})
	if err != nil || resp == nil {
		return nil
	}
	return resp.Hash
}

func (c *SclClient) GetStateRootDetailed() ([]byte, error) {
	// [TIMEOUT-FIX] Áp dụng timeout 10 giây để chống nghẽn vĩnh viễn
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetStateRoot(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}
	return resp.Hash, nil
}

func (c *SclClient) GetSpendableBalance(addr []byte) uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetSpendableBalance(ctx, &pb.BalanceRequest{Address: addr})
	if err != nil {
		log.Printf("[SclClient] ⚠️ Lỗi gRPC khi GetSpendableBalance: %v", err)
		return 0
	}
	if resp == nil {
		return 0
	}
	return resp.Balance
}

func (c *SclClient) GetOldestHeight() uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetOldestHeight(ctx, &pb.Empty{})
	if err != nil {
		return 0
	}
	return resp.Value
}

func (c *SclClient) GetMedianTimePast(height uint64) uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetMedianTimePast(ctx, &pb.Uint64Request{Value: height})
	if err != nil {
		return 0
	}
	return resp.Value
}

func (c *SclClient) GetFinalizedHeight() uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetFinalizedHeight(ctx, &pb.Empty{})
	if err != nil || resp == nil {
		return 0
	}
	return resp.Value
}

func (c *SclClient) SetFinalizedHeight(h uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := c.authCtx(ctx)
	c.client.SetFinalizedHeight(authCtx, &pb.Uint64Request{Value: h})
}

func (c *SclClient) ForceSetFinalizedHeight(h uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	authCtx := c.authCtx(ctx)
	c.client.ForceSetFinalizedHeight(authCtx, &pb.Uint64Request{Value: h})
}

func (c *SclClient) GetCurrentVersion() uint64 {
	if c.client == nil {
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.GetCurrentVersion(ctx, &pb.Empty{})
	if err != nil || resp == nil {
		return 0
	}
	return resp.Value
}

func (c *SclClient) PurgeHistoricalData(start, end uint64) (bool, error) {
	if c.client == nil {
		return false, fmt.Errorf("client not initialized")
	}

	// Ä Ă³ng gĂ³i 16 bytes: 8 bytes start + 8 bytes end (BigEndian)
	data := make([]byte, 16)
	binary.BigEndian.PutUint64(data[0:8], start)
	binary.BigEndian.PutUint64(data[8:16], end)

	// [SECURITY-HARDENING] Gá»­i auth token trong metadata cho destructive API
	// [MAINNET-TIMEOUT] Tăng timeout lên 60 giây vì việc xóa dữ liệu hàng triệu khối trong RocksDB
	// có thể kích hoạt Compaction gây trễ ổ đĩa, timeout 5 giây cũ sẽ làm sập kết nối gRPC.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	authCtx := c.authCtx(ctx)
	resp, err := c.client.PurgeHistoricalData(authCtx, &pb.BytesRequest{
		Data: data,
	})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *SclClient) GetTransactionStatus(hash []byte) (uint64, uint32, bool, uint64, uint64, uint64, uint64, uint64) {
	// [TIMEOUT-FIX] Áp dụng timeout 10 giây để Go tự ngắt nếu SCL Core bị nghẽn
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetTransactionStatus(ctx, &pb.HashRequest{Hash: hash})
	if err != nil {
		return 0, 0, false, 0, 0, 0, 0, 0
	}
	return resp.Height, resp.Status, resp.IsFinalized, resp.Confirmations, resp.SenderPrevBalance, resp.SenderPostBalance, resp.ReceiverPrevBalance, resp.ReceiverPostBalance
}

func (c *SclClient) ExecuteBlock(body []byte, miner []byte, parent []byte, height uint64, isSimulation bool, timestamp uint64) ([]byte, bool, string, int32) {
	log.Printf("[gRPC-DEBUG] Sending ExecuteBlock for height: %d, timestamp: %d", height, timestamp)
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // Tăng lên 120s cho khối 35MB
	defer cancel()

	// [SECURITY-HARDENING] Sá»­ dá»¥ng authCtx cho phÆ°Æ¡ng thá»©c ghi dá»¯ liá»‡u quan trá» ng
	authCtx := c.authCtx(ctx)
	resp, err := c.client.ExecuteBlock(authCtx, &pb.SclBlockExecutionRequest{
		BodyRaw:      body,
		MinerAddress: miner,
		ParentHash:   parent,
		Height:       height,
		IsSimulation: isSimulation,
		Timestamp:    timestamp,
	})
	if err != nil {
		log.Printf("[gRPC-DEBUG] ❌ ExecuteBlock RPC Failed for height %d: %v", height, err)
		return nil, false, err.Error(), -1
	}
	log.Printf("[gRPC-DEBUG] ✅ ExecuteBlock RPC Success for height %d: Success=%v, FailingTX=%d", height, resp.Success, resp.FailingTxIndex)
	return resp.StateRoot, resp.Success, resp.ErrorMsg, resp.FailingTxIndex
}

func (c *SclClient) CommitBlockHash(height uint64, hash []byte) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	_, err := c.client.CommitBlockHash(authCtx, &pb.CommitHashRequest{Height: height, Hash: hash})
	if err != nil {
		log.Printf("[SclClient] ❌ Lỗi gọi CommitBlockHash RPC: %v", err)
	}
}

func (c *SclClient) RollbackState(current uint64, target uint64) bool {
	// [MAINNET-TIMEOUT] Tăng timeout lên 300 giây (5 phút) vì khi xảy ra tái cấu trúc chuỗi (Reorg) sâu
	// trên database 50GB+, Rust Core phải xóa lượng lớn JMT Nodes cũ và cập nhật số dư, thao tác này mất nhiều thời gian I/O.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.RollbackState(authCtx, &pb.RollbackRequest{
		CurrentHeight: current, TargetHeight: target,
	})
	if err != nil {
		return false
	}
	return resp.Success
}

// [BÀN TAY VÔ HÌNH] Xóa khối vật lý — bỏ qua Tường lửa Bất biến.
// Chỉ dùng cho công cụ nhà vận hành node (localhost + mã xác nhận 01900).
func (c *SclClient) ForceDeleteBlocks(current uint64, target uint64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // 120s cho xóa nhiều khối
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.ForceDeleteBlocks(authCtx, &pb.RollbackRequest{
		CurrentHeight: current, TargetHeight: target,
	})
	if err != nil {
		return false
	}
	return resp.Success
}



func (c *SclClient) CalculateNextDifficulty(timestamps []uint64, difficulties [][]byte, currentTs uint64, height uint64) []byte {
	// [TIMEOUT-FIX] Áp dụng timeout 10 giây để thợ đào không bị kẹt vô hạn
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.CalculateNextDifficulty(ctx, &pb.DifficultyRequest{
		Timestamps: timestamps, Difficulties: difficulties, CurrentTs: currentTs, Height: height,
	})
	if err != nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) VerifyPow(headerBytes []byte, nonce uint64, diff []byte, height uint64) int32 {
	// [TIMEOUT-FIX] Áp dụng timeout 10 giây để chống nghẽn luồng xác thực khối mới
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.VerifyPow(ctx, &pb.VerifyPowRequest{
		HeaderBytes: headerBytes,
		Nonce:       nonce,
		Difficulty:  diff,
		Height:      height,
	})
	if err != nil || resp == nil {
		return -1
	}
	return resp.Result
}

func (c *SclClient) GetHashrate() uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.client.GetHashrate(ctx, &pb.Empty{})
	if err != nil || resp == nil {
		return 0
	}
	return resp.Value
}

func (c *SclClient) CalculateAbsoluteWeight(parent []byte, diff []byte) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := c.client.CalculateAbsoluteWeight(ctx, &pb.WeightRequest{ParentWeight: parent, Difficulty: diff})
	if err != nil {
		return nil
	}
	return resp.Data
}



func (c *SclClient) CalculateShortTxId(txHash []byte, nonce uint64) uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.client.CalculateShortTxId(ctx, &pb.ShortIdRequest{TxHash: txHash, Nonce: nonce})
	if err != nil {
		return 0
	}
	return resp.Value
}

func (c *SclClient) VerifyBlockReconstruction(root []byte, hashes [][]byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := c.client.VerifyBlockReconstruction(ctx, &pb.ReconstructionRequest{ExpectedTxRoot: root, TxHashes: hashes})
	if err != nil {
		return false
	}
	return resp.Value
}

func (c *SclClient) VerifyTimestampFirewall(ts, mtp, now uint64) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	resp, err := c.client.VerifyTimestampFirewall(ctx, &pb.TimestampFirewallRequest{
		Timestamp:      ts,
		MedianTimePast: mtp,
		CurrentNow:     now,
	})
	if err != nil {
		return false
	}
	return resp.Value
}

func (c *SclClient) VerifySignature(address, message, signature []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.VerifySignature(ctx, &pb.SignatureCheckRequest{
		Address:   address,
		Message:   message,
		Signature: signature,
	})
	if err != nil || resp == nil {
		return false
	}
	return resp.Value
}

func (c *SclClient) ImportStateSnapshot(data []byte, version uint64) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second) // Tăng lên 10 phút cho Snapshot GBs
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.ImportStateSnapshot(authCtx, &pb.SnapshotRequest{Data: data, Version: version})
	if err != nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) ImportStateSnapshotPath(path string, version uint64) []byte {
	// [TIÊU CHUẨN ZERO TECHNICAL DEBT] Sử dụng authCtx để truyền Auth Token cho cuộc gọi gRPC nạp snapshot từ đường dẫn file
	// Tại sao: Nhằm đảm bảo gRPC request chứa token xác thực hợp lệ, vượt qua chốt chặn bảo mật của Rust Core 
	// và tránh lỗi gRPC AUTH FAILED gây kẹt tiến trình SnapSync.
	// [MAINNET-TIMEOUT] Tăng timeout lên 3600 giây (1 giờ) vì việc dựng lại cây JMT Merkle
	// từ 50GB dữ liệu snapshot phẳng (hàng trăm triệu tài khoản) yêu cầu hàng tỷ phép băm và I/O ghi đĩa rất lớn.
	ctx, cancel := context.WithTimeout(context.Background(), 3600*time.Second)
	defer cancel()
	authCtx := c.authCtx(ctx)
	resp, err := c.client.ImportStateSnapshotPath(authCtx, &pb.SnapshotPathRequest{Path: path, Version: version})
	if err != nil {
		log.Printf("[SclClient] ❌ Lỗi gọi ImportStateSnapshotPath: %v", err)
		return nil
	}
	return resp.Data
}


func (c *SclClient) CalculateExpectedSupply(h uint64) uint64 {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, _ := c.client.CalculateExpectedSupply(ctx, &pb.Uint64Request{Value: h})
	if resp != nil {
		return resp.Value
	}
	return 0
}

func (c *SclClient) SetExpectedSupply(supply uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	c.client.SetExpectedSupply(authCtx, &pb.Uint64Request{Value: supply})
}

func (c *SclClient) CalculateActualTotalSupply() uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := c.client.CalculateActualTotalSupply(ctx, &pb.Empty{})
	if err != nil {
		log.Printf("[SclClient] ⚠️ Lỗi gRPC khi CalculateActualTotalSupply: %v", err)
		return 0
	}
	if resp != nil {
		return resp.Value
	}
	return 0
}

func (c *SclClient) GetActualTotalSupply() uint64 {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := c.client.GetActualTotalSupply(ctx, &pb.Empty{})
	if err != nil {
		log.Printf("[SclClient] ⚠️ Lỗi gRPC khi GetActualTotalSupply: %v", err)
		return 0
	}
	if resp != nil {
		return resp.Value
	}
	return 0
}

func (c *SclClient) DebugDumpSmtNodes() string {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.DebugDumpSmtNodes(ctx, &pb.Empty{})
	if err != nil {
		log.Printf("[SclClient] ⚠️ Lỗi gRPC khi DebugDumpSmtNodes: %v", err)
		return ""
	}
	if resp != nil {
		return resp.Value
	}
	return ""
}

func (sm *SclClient) ExportStateSnapshotRaw() []byte {
	// [SNAPSHOT-TIMEOUT-FIX] Tăng timeout lên 300 giây (5 phút) để phòng ngừa nghẽn I/O khi số lượng ví phình to ở đỉnh.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()
	resp, err := sm.client.ExportStateSnapshot(ctx, &pb.Empty{})
	if err != nil {
		log.Printf("[SclClient] ❌ Lỗi gọi ExportStateSnapshot RPC: %v", err)
		return nil
	}
	if resp == nil {
		log.Printf("[SclClient] ❌ Lỗi gọi ExportStateSnapshot RPC: Response rỗng")
		return nil
	}
	return resp.Data
}

func (sm *SclClient) ExportStateSnapshotAtHeightRaw(height uint64) []byte {
	// [SNAPSHOT-TIMEOUT-FIX] Tăng timeout lên 600 giây (10 phút) vì quá trình duyệt cây JMT 
	// cho hàng chục ngàn tài khoản ở phiên bản cũ (version < current_v) gây ra hàng triệu I/O đọc RocksDB, 
	// dễ dẫn đến timeout 120 giây cũ trên các ổ đĩa thông thường.
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second)
	defer cancel()
	
	log.Printf("[SclClient] 📡 Bắt đầu gọi RPC ExportStateSnapshotAtHeight cho cao độ #%d...", height)
	resp, err := sm.client.ExportStateSnapshotAtHeight(ctx, &pb.Uint64Request{Value: height})
	if err != nil {
		log.Printf("[SclClient] ❌ Lỗi gọi ExportStateSnapshotAtHeight RPC tại #%d: %v", height, err)
		return nil
	}
	if resp == nil {
		log.Printf("[SclClient] ❌ Lỗi gọi ExportStateSnapshotAtHeight RPC tại #%d: Response rỗng", height)
		return nil
	}
	
	log.Printf("[SclClient] ✅ Nhận phản hồi RPC ExportStateSnapshotAtHeight tại #%d thành công. Độ dài dữ liệu (resp.Data): %d bytes", height, len(resp.Data))
	return resp.Data
}

func (c *SclClient) ExportStateSnapshot() []AccountSnapshot {
	data := c.ExportStateSnapshotRaw()
	if data == nil {
		return nil
	}
	var snapshot []AccountSnapshot
	err := borsh.Deserialize(&snapshot, data)
	if err != nil {
		return nil
	}
	return snapshot
}

func (c *SclClient) GetAddressType(addr []byte) int32 {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.GetAddressType(ctx, &pb.BytesRequest{Data: addr})
	if err != nil {
		return -1
	}
	return resp.Value
}

func (c *SclClient) IsValidFee(fee uint64) bool {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.IsValidFee(ctx, &pb.Uint64Request{Value: fee})
	if err != nil {
		return false
	}
	return resp.Value
}

func (c *SclClient) CalculateNanoFee(amount uint64, weight uint32) uint64 {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.CalculateNanoFee(ctx, &pb.NanoFeeRequest{Amount: amount, Weight: weight})
	if err != nil {
		return 0
	}
	return resp.Value
}

func (c *SclClient) GetNanoWeight(rawTx []byte) uint64 {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.GetNanoWeight(ctx, &pb.BytesRequest{Data: rawTx})
	if err != nil {
		return 0
	}
	return resp.Value
}

func (c *SclClient) CalculateTxHash(data []byte, height uint64) []byte {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.CalculateTxHashWithHeight(ctx, &pb.CalculateTxHashRequest{
		Data:   data,
		Height: height,
	})
	if err != nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) CalculateBlake3HashWithHeight(data []byte, height uint64) []byte {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.CalculateBlake3HashWithHeight(ctx, &pb.CalculateBlake3HashRequest{
		Data:   data,
		Height: height,
	})
	if err != nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) CalculateBlockHeaderHash(data []byte) []byte {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.CalculateBlockHeaderHash(ctx, &pb.BytesRequest{Data: data})
	if err != nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) CalculateSigningHash(tx *pb.Transaction) []byte {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.CalculateSigningHash(ctx, &pb.CalculateSigningHashRequest{
		Transaction: tx,
	})
	if err != nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) GetHeaderRaw(hash []byte) []byte {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.GetHeaderRaw(ctx, &pb.BytesRequest{Data: hash})
	if err != nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) CalculateMerkleRoot(flatHashes []byte) []byte {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.CalculateMerkleRoot(ctx, &pb.MerkleRootRequest{FlatHashes: flatHashes})
	if err != nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) CalculateBlockRewardBtcZ(h uint64) uint64 {
	// [TIMEOUT-FIX] Áp dụng timeout 5 giây
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.CalculateBlockRewardBtcZ(ctx, &pb.Uint64Request{Value: h})
	if err != nil || resp == nil {
		return 0
	}
	return resp.Value
}

// [V19 UNIFIED STORAGE] Hệ thống Quản trị Sổ cái Nhất thể (Rust Core)

func (c *SclClient) GetBlockRaw(height uint64) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.GetBlockRaw(ctx, &pb.Uint64Request{Value: height})
	if err != nil || resp == nil {
		// [SILENT-FAIL] Không ghi log vì Header-Only blocks (dưới vùng snapshot) không có body raw
		// Đây là hành vi bình thường trong chế độ Fast Sync, không phải lỗi
		return nil
	}
	return resp.Data
}

func (c *SclClient) GetBlockHash(height uint64) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.GetBlockHash(ctx, &pb.Uint64Request{Value: height})
	if err != nil || resp == nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) GetRawByHash(hash []byte) []byte {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := c.client.GetRawByHash(ctx, &pb.BytesRequest{Data: hash})
	if err != nil || resp == nil {
		return nil
	}
	return resp.Data
}

func (c *SclClient) SaveBlockRaw(height uint64, hash []byte, data []byte, isCanonical bool) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.SaveBlockRaw(authCtx, &pb.SaveBlockRequest{
		Height:      height,
		Hash:        hash,
		Data:        data,
		IsCanonical: isCanonical,
	})
	if err != nil || resp == nil {
		log.Printf("[SclClient] ❌ Lỗi khi lưu BlockRaw cho Height #%d: %v", height, err)
		return false
	}
	return resp.Value
}

func (c *SclClient) AuthoritativeSign(data []byte, privKey []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.AuthoritativeSign(authCtx, &pb.SignRequest{
		Data:       data,
		PrivateKey: privKey,
	})
	if err != nil {
		return nil, err
	}
	if !resp.Success {
		return nil, fmt.Errorf("%s", resp.ErrorMsg)
	}
	return resp.Signature, nil
}

func (c *SclClient) DeleteByHash(hash []byte) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.DeleteByHash(authCtx, &pb.HashRequest{Hash: hash})
	if err != nil || resp == nil {
		return false
	}
	return resp.Success
}

func (c *SclClient) PrepareTransaction(sender, receiver []byte, amount, fee, nonce uint64, privKey []byte, recentHash []byte) (*pb.Transaction, error) {
	// [STRESS-TEST-TIMEOUT-FIX] Tăng timeout gRPC lên 60 giây để xử lý bão concurrent streams khi stress test online (spam_tx.py 200 TX/s)
	// Tại sao: Khi client gửi lô lớn 200 giao dịch chưa ký, việc gọi đồng thời 200 gRPC PrepareTransaction 
	// qua h2 connection gây hàng đợi xếp stream và dễ bị hủy do timeout 5s cũ.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	return c.client.PrepareTransaction(ctx, &pb.PrepareTxRequest{
		Sender:          sender,
		Receiver:        receiver,
		Amount:          amount,
		Fee:             fee,
		Nonce:           nonce,
		PrivateKey:      privKey,
		RecentBlockHash: recentHash,
	})
}

func (c *SclClient) EmergencyStateRebuild(version uint64) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 600*time.Second) // Tăng lên 10 phút
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.EmergencyStateRebuild(authCtx, &pb.Uint64Request{Value: version})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *SclClient) ResetStateCompletely() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.ResetStateCompletely(authCtx, &pb.Empty{})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *SclClient) ClearStagingArea() (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.ClearStagingArea(authCtx, &pb.Empty{})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *SclClient) GetHighestBlockHeight() (uint64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	resp, err := c.client.GetHighestBlockHeight(authCtx, &pb.Empty{})
	if err != nil {
		return 0, err
	}
	return resp.Value, nil
}

func (c *SclClient) AddToMempool(txHash []byte, txRaw []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.AddToMempool(ctx, &pb.MempoolEntry{
		TxHash: txHash,
		TxRaw:  txRaw,
	})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *SclClient) AddBatchToMempool(hashes [][]byte, raws [][]byte) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	entries := make([]*pb.MempoolEntry, len(hashes))
	for i := range hashes {
		entries[i] = &pb.MempoolEntry{
			TxHash: hashes[i],
			TxRaw:  raws[i],
		}
	}

	// Tại sao: Sử dụng gRPC client để gửi cả mảng giao dịch sang Rust Server chỉ trong 1 call duy nhất nhằm tối ưu hóa hiệu năng
	resp, err := c.client.AddBatchToMempool(ctx, &pb.MempoolEntriesResponse{
		Entries: entries,
	})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *SclClient) RemoveFromMempool(txHash []byte) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.RemoveFromMempool(ctx, &pb.HashRequest{Hash: txHash})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *SclClient) RemoveFromMempoolBatch(txHashes [][]byte) (bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.RemoveFromMempoolBatch(ctx, &pb.RemoveBatchFromMempoolRequest{Hashes: txHashes})
	if err != nil {
		return false, err
	}
	return resp.Success, nil
}

func (c *SclClient) GetMempoolEntries() ([]*pb.MempoolEntry, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetMempoolEntries(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.Entries, nil
}

func (c *SclClient) GetTransactionsByAddress(addr []byte) ([]*pb.TrackedTx, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	resp, err := c.client.GetTransactionsByAddress(ctx, &pb.AddressRequest{Address: addr})
	if err != nil {
		return nil, err
	}
	return resp.Transactions, nil
}

func (c *SclClient) GetNodeConfig() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.client.GetNodeConfig(ctx, &pb.Empty{})
	if err != nil {
		return nil, err
	}
	return resp.Data, nil
}

func (c *SclClient) SetNodeConfig(data []byte) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, err := c.client.SetNodeConfig(ctx, &pb.ConfigRequest{Data: data})
	return err
}

func (c *SclClient) EvaluateHeaderChain(headers [][]byte) (*pb.EvaluateHeaderChainResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second) // Tăng lên 60s
	defer cancel()
	return c.client.EvaluateHeaderChain(ctx, &pb.EvaluateHeaderChainRequest{
		HeadersRaw: headers,
	})
}

func (c *SclClient) ProcessNewBlock(blockRaw []byte) (*pb.ProcessNewBlockResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second) // Tăng lên 120s cho khối 35MB
	defer cancel()
	return c.client.ProcessNewBlock(ctx, &pb.ProcessNewBlockRequest{
		BlockRaw: blockRaw,
	})
}



func (c *SclClient) ProcessChain(blocksRaw [][]byte) (*pb.SyncChainResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute) // [Stress-Fix] Tăng lên 5 phút cho các khối cực nặng
	defer cancel()

	deadline := time.Now().Add(5*time.Minute).Unix() - 5 // Trừ hao 5s để đảm bảo dừng trước client timeout
	ctx = metadata.AppendToOutgoingContext(ctx,
		"x-is-syncing", "true",
		"x-deadline", fmt.Sprintf("%d", deadline),
	)

	return c.client.ProcessChain(ctx, &pb.SyncChainRequest{
		BlocksRaw: blocksRaw,
	})
}
func (c *SclClient) BuildVanguardBlockTemplate(height uint64, parentHash []byte, minerAddr []byte, txsBytes [][]byte, ts uint64, diff []byte) ([]byte, int32, string) {
	// Ä á»‹nh nghÄ©a struct Ä‘á»ƒ Borsh Encode
	type InternalBuildRequest struct {
		Height            uint64
		ParentHash        []byte
		MinerAddress      []byte
		TransactionsBytes [][]byte
		Timestamp         uint64
		DifficultyRaw     []byte
	}

	type BlockTemplateResult struct {
		BlockRaw       []byte
		Success        bool
		ErrorMsg       string
		FailingTxIndex int32
	}

	reqData := InternalBuildRequest{
		Height:            height,
		ParentHash:        parentHash,
		MinerAddress:      minerAddr,
		TransactionsBytes: txsBytes,
		Timestamp:         ts,
		DifficultyRaw:     diff,
	}

	borshData, err := borsh.Serialize(reqData)
	if err != nil {
		log.Printf("[SclClient] ❌ Lỗi Borsh Encode BuildVanguardBlockTemplate: %v", err)
		return nil, -1, err.Error()
	}

	// [MAINNET-TIMEOUT] Tăng timeout lên 300 giây để đảm bảo thợ đào có đủ thời gian thực hiện dry-run mô phỏng
	// chạy thử toàn bộ giao dịch trong khối (lên tới 140.000 giao dịch cho 35MB) để tính toán StateRoot JMT tuần tự.
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
	defer cancel()

	resp, err := c.client.BuildVanguardBlockTemplate(ctx, &pb.BytesRequest{
		Data: borshData,
	})
	if err != nil || resp == nil || len(resp.Data) == 0 {
		log.Printf("[SclClient] ❌ Lỗi gọi BuildVanguardBlockTemplate hoặc Data rỗng: %v", err)
		return nil, -1, ""
	}

	var res BlockTemplateResult
	if err := borsh.Deserialize(&res, resp.Data); err != nil {
		log.Printf("[SclClient] ❌ Lỗi Borsh Decode BlockTemplateResult (Size: %d): %v", len(resp.Data), err)
		return nil, -1, "Borsh Decode Error"
	}

	if !res.Success {
		return nil, res.FailingTxIndex, res.ErrorMsg
	}

	return res.BlockRaw, -1, ""
}

func (c *SclClient) ReindexMinerHistory(addr []byte) error {
	// [TIMEOUT-FIX] Thay thế context.Background() bằng timeout 30 phút (1800 giây)
	// để ngăn chặn rủi ro kẹt luồng vô hạn nhưng vẫn cung cấp thời gian cực dài cho tác vụ quét lịch sử nặng.
	ctx, cancel := context.WithTimeout(context.Background(), 1800*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	_, err := c.client.ReindexMinerHistory(authCtx, &pb.AddressRequest{Address: addr})
	return err
}



func (c *SclClient) ValidateTransactionBatch(rawTxs [][]byte) (*pb.ValidateTxBatchResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	return c.client.ValidateTransactionBatch(authCtx, &pb.ValidateTxBatchRequest{
		RawTxs: rawTxs,
	})
}

func (c *SclClient) GetTransactionStatusBatch(hashes [][]byte) (*pb.BatchTxStatusResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	return c.client.GetTransactionStatusBatch(authCtx, &pb.BatchTxStatusRequest{
		Hashes: hashes,
	})
}

func (c *SclClient) GetBalanceBatch(addresses [][]byte) (*pb.BatchBalanceResponse, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	authCtx := c.authCtx(ctx)
	return c.client.GetBalanceBatch(authCtx, &pb.BatchBalanceRequest{
		Addresses: addresses,
	})
}

// CalculateTxHashesBatch gửi lô giao dịch thô xuống Rust Core để băm song song, tối ưu hóa gRPC storm.
func (c *SclClient) CalculateTxHashesBatch(rawTxs [][]byte, height uint64) ([][]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	resp, err := c.client.CalculateTxHashesBatch(ctx, &pb.BatchTxHashRequest{
		RawTxs: rawTxs,
		Height: height,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, fmt.Errorf("response is nil")
	}
	return resp.Hashes, nil
}

func (c *SclClient) WatchCoreEvents(ctx context.Context) (pb.SclService_WatchCoreEventsClient, error) {
	// Dùng context không timeout vì đây là luồng treo vĩnh viễn (Long-lived connection)
	authCtx := c.authCtx(ctx) 
	return c.client.WatchCoreEvents(authCtx, &pb.Empty{})
}

