//go:build windows && 386

package driver

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/windows/registry"
)

var (
	UsbDeviceDLL syscall.Handle
)

func init() {
	loadDLLs()
}

func loadDLLs() {
	// Try to load from registry first
	if registryPath := getRegistryPath(); registryPath != "" {
		fmt.Println("Found registry path:", registryPath)
		// Load libusb-1.0.dll first (dependency)
		libusbPath := filepath.Join(registryPath, "libusb-1.0.dll")
		if _, err := syscall.LoadLibrary(libusbPath); err != nil {
			fmt.Println("Warning: Failed to load libusb-1.0.dll from", libusbPath, "Error:", err)
		}

		// Then load USB2XXX.dll
		usbPath := filepath.Join(registryPath, "USB2XXX.dll")
		if handle, err := syscall.LoadLibrary(usbPath); err == nil {
			UsbDeviceDLL = handle
			fmt.Println("Loaded x86 DLLs from registry path:", registryPath)
			return
		} else {
			fmt.Println("Failed to load USB2XXX.dll from", usbPath, "Error:", err)
		}
	} else {
		fmt.Println("Registry path not found")
	}

	// Fallback to hardcoded path
	_, _ = syscall.LoadLibrary(".\\DLLs\\windows_x86\\libusb-1.0.dll")
	UsbDeviceDLL, _ = syscall.LoadLibrary(".\\DLLs\\windows_x86\\USB2XXX.dll")
	fmt.Println("Loaded x86 DLLs from default path")
}

func getRegistryPath() string {
	const uninstall = `SOFTWARE\Microsoft\Windows\CurrentVersion\Uninstall`

	views := []struct {
		label  string
		access uint32
	}{
		{"64", registry.READ | registry.WOW64_64KEY},
		{"32", registry.READ | registry.WOW64_32KEY},
		{"default", registry.READ},
	}

	for _, view := range views {
		if path := findRegistryPathInView(uninstall, view.label, view.access); path != "" {
			return path
		}
	}

	return ""
}

func dirFromUninstallString(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	s = strings.Trim(s, `"`)
	if i := strings.IndexByte(s, ' '); i > 0 {
		s = s[:i]
	}
	s = strings.Trim(s, `"`)
	if s == "" {
		return ""
	}
	return filepath.Dir(s)
}

func findRegistryPathInView(uninstall, label string, access uint32) string {
	k, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstall, access)
	if err != nil {
		fmt.Println("OpenKey HKLM", label, "view failed:", err)
		return ""
	}
	defer k.Close()

	names, err := k.ReadSubKeyNames(-1)
	if err != nil {
		fmt.Println("ReadSubKeyNames failed:", err)
		return ""
	}

	fmt.Println("HKLM", label, "view entries:", len(names))

	for _, name := range names {
		sk, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstall+`\`+name, access)
		if err != nil {
			continue
		}

		publisher, _, _ := sk.GetStringValue("Publisher")
		displayName, _, _ := sk.GetStringValue("DisplayName")
		install, _, _ := sk.GetStringValue("InstallLocation")
		appPath, _, _ := sk.GetStringValue("Inno Setup: App Path")
		unins, _, _ := sk.GetStringValue("UninstallString")
		sk.Close()

		pubL := strings.ToLower(strings.TrimSpace(publisher))
		dnL := strings.ToLower(strings.TrimSpace(displayName))

		if strings.Contains(pubL, "toomoss") || strings.Contains(dnL, "toomoss") {
			fmt.Println("Matched subkey:", name)
			fmt.Println("  DisplayName:", displayName)
			fmt.Println("  Publisher:", publisher)

			install = strings.TrimSpace(install)
			if install != "" {
				fmt.Println("  InstallLocation:", install)
				return filepath.Clean(install)
			}

			appPath = strings.TrimSpace(appPath)
			if appPath != "" {
				fmt.Println("  AppPath:", appPath)
				return filepath.Clean(appPath)
			}

			if dir := dirFromUninstallString(unins); dir != "" {
				fmt.Println("  From UninstallString:", dir)
				if hasUSB2XXXDLL(dir) {
					return dir
				}
				fmt.Println("  UninstallString path missing USB2XXX.dll")
			}

			fmt.Println("  No usable path fields")
		}
	}

	// Second pass: locate by path hints even if publisher/display name do not match.
	for _, name := range names {
		sk, err := registry.OpenKey(registry.LOCAL_MACHINE, uninstall+`\`+name, access)
		if err != nil {
			continue
		}

		install, _, _ := sk.GetStringValue("InstallLocation")
		appPath, _, _ := sk.GetStringValue("Inno Setup: App Path")
		unins, _, _ := sk.GetStringValue("UninstallString")
		sk.Close()

		install = strings.TrimSpace(install)
		if install != "" && pathLooksToomoss(install) {
			fmt.Println("Matched InstallLocation by path hint:", name)
			return filepath.Clean(install)
		}

		appPath = strings.TrimSpace(appPath)
		if appPath != "" && pathLooksToomoss(appPath) {
			fmt.Println("Matched AppPath by path hint:", name)
			return filepath.Clean(appPath)
		}

		if dir := dirFromUninstallString(unins); dir != "" && pathLooksToomoss(dir) {
			fmt.Println("Matched UninstallString by path hint:", name)
			return dir
		}
	}

	return ""
}

func hasUSB2XXXDLL(dir string) bool {
	if dir == "" {
		return false
	}
	_, err := os.Stat(filepath.Join(dir, "USB2XXX.dll"))
	return err == nil
}

func pathLooksToomoss(p string) bool {
	pl := strings.ToLower(p)
	return strings.Contains(pl, "toomoss") || strings.Contains(pl, "tcanlinpro")
}
