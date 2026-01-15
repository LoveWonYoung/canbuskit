//go:build windows && 386

package driver

import (
	"fmt"
	"syscall"
)

var (
	_, _            = syscall.LoadLibrary(".\\DLLs\\windows_x86\\libusb-1.0.dll")
	UsbDeviceDLL, _ = syscall.LoadLibrary(".\\DLLs\\windows_x86\\USB2XXX.dll")
)

func init() {
	fmt.Println("Loaded x86	 DLLs")
}
