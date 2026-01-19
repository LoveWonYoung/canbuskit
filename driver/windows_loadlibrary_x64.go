//go:build windows && amd64

package driver

import (
	"fmt"
	"syscall"
)

var (
	_, _            = syscall.LoadLibrary(".\\DLLs\\windows_x64\\libusb-1.0.dll")
	UsbDeviceDLL, _ = syscall.LoadLibrary(".\\DLLs\\windows_x64\\USB2XXX.dll")
)

func init() {
	fmt.Println("Loaded x64	 DLLs")
	loadProcAddresses()
}
