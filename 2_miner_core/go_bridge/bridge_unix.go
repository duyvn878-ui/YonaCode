//go:build !windows
// +build !windows

package go_bridge

// assignProcessToJob trả về 0 trên hệ điều hành phi Windows vì Job Objects
// là cơ chế đặc thù riêng biệt của Windows.
func assignProcessToJob(pid int) uintptr {
	return 0
}
