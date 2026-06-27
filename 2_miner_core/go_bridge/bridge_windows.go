//go:build windows
// +build windows

package go_bridge

import (
	"unsafe"
	"golang.org/x/sys/windows"
)

// assignProcessToJob liên kết tiến trình SCL Server với tiến trình Go
// để đảm bảo SCL Server sẽ tự động bị tắt khi ứng dụng chính đóng.
func assignProcessToJob(pid int) uintptr {
	job, err := windows.CreateJobObject(nil, nil)
	if err == nil {
		info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{
			BasicLimitInformation: windows.JOBOBJECT_BASIC_LIMIT_INFORMATION{
				LimitFlags: windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE,
			},
		}
		windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
		hProcess, err := windows.OpenProcess(windows.PROCESS_ALL_ACCESS, false, uint32(pid))
		if err == nil {
			windows.AssignProcessToJobObject(job, hProcess)
			return uintptr(job)
		}
	}
	return 0
}
