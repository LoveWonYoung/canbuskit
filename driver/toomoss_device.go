//go:build windows

package driver

import (
	"syscall"
	"unsafe"
)

var (
	UsbScanDevice, _  = syscall.GetProcAddress(UsbDeviceDLL, "USB_ScanDevice")
	UsbOpenDevice, _  = syscall.GetProcAddress(UsbDeviceDLL, "USB_OpenDevice")
	UsbCloseDevice, _ = syscall.GetProcAddress(UsbDeviceDLL, "USB_CloseDevice")
	DevHandle         [10]int
	DEVIndex          = 0
)

func UsbScan() bool {
	ret2, _, _ := syscall.SyscallN(
		UsbScanDevice,
		uintptr(unsafe.Pointer(&DevHandle[DEVIndex])),
	)
	return ret2 > 0
}

func UsbOpen() bool {
	stateValue, _, _ := syscall.SyscallN(
		UsbOpenDevice,
		uintptr(DevHandle[DEVIndex]))
	return stateValue >= 1
}

func UsbClose() bool {
	ret, _, _ := syscall.SyscallN(
		UsbCloseDevice,
		uintptr(DevHandle[DEVIndex]))
	_ = syscall.FreeLibrary(UsbDeviceDLL)
	return ret >= 1
}
