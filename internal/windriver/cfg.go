package driver

import "fmt"

func normalizeConfig(cfg Config) (Config, error) {
	return cfg.Normalized()
}

func validateWrite(cfg Config, id int32, fd bool, data []byte) error {
	if id < 0 || id > 0x7FF {
		return fmt.Errorf("standard CAN ID 0x%X out of range (0x000-0x7FF)", id)
	}
	if fd && cfg.Mode != CANFD {
		return fmt.Errorf("driver is configured for classic CAN")
	}
	if !fd && len(data) > 8 {
		return fmt.Errorf("data length %d exceeds CAN maximum of 8", len(data))
	}
	if fd && len(data) > 64 {
		return fmt.Errorf("data length %d exceeds CAN-FD maximum of 64", len(data))
	}
	return nil
}
