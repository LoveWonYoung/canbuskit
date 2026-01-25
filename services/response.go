package services

import (
	"errors"
	"fmt"
)

func validateResponseSID(resp []byte, expectedSID byte) error {
	if len(resp) == 0 {
		return fmt.Errorf("short response: % X", resp)
	}
	if resp[0] == 0x7F {
		if len(resp) >= 3 {
			return fmt.Errorf("negative response to 0x%02X: 0x%02X", resp[1], resp[2])
		}
		return errors.New("negative response")
	}
	if resp[0] != expectedSID {
		return fmt.Errorf("unexpected response: % X", resp)
	}
	return nil
}

func validatePositiveResponse(resp []byte, serviceID, subFunc byte) error {
	if len(resp) < 2 {
		return fmt.Errorf("short response: % X", resp)
	}
	expectedSID := serviceID + 0x40
	if err := validateResponseSID(resp, expectedSID); err != nil {
		return err
	}
	if resp[1] != subFunc {
		return fmt.Errorf("unexpected response: % X", resp)
	}
	return nil
}
