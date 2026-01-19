//go:build windows

package driver

import (
	"syscall"
	"unsafe"
)

var (
	UsbScanDevice  uintptr
	UsbOpenDevice  uintptr
	UsbCloseDevice uintptr
	DevHandle      [10]int
	DEVIndex       = 0
)

func loadProcAddresses() {
	UsbScanDevice, _ = syscall.GetProcAddress(UsbDeviceDLL, "USB_ScanDevice")
	UsbOpenDevice, _ = syscall.GetProcAddress(UsbDeviceDLL, "USB_OpenDevice")
	UsbCloseDevice, _ = syscall.GetProcAddress(UsbDeviceDLL, "USB_CloseDevice")

	CANFDInit, _ = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_Init")
	CANFDStartGetMsg, _ = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_StartGetMsg")
	CANFD_GetMsg, _ = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_GetMsg")
	CANFD_SendMsg, _ = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_SendMsg")
	CANFD_GetCANSpeedArg, _ = syscall.GetProcAddress(UsbDeviceDLL, "CANFD_GetCANSpeedArg")
}

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
