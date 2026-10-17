package driver

import "testing"

func TestCANFDInitConfigValidateTiming(t *testing.T) {
	ok := CANFDInitConfig{
		NBT_SEG1: 59, NBT_SEG2: 20, NBT_SJW: 2,
		DBT_SEG1: 14, DBT_SEG2: 5, DBT_SJW: 2,
	}
	if err := ok.ValidateTiming(); err != nil {
		t.Fatalf("ValidateTiming() unexpected error: %v", err)
	}

	bad := CANFDInitConfig{NBT_SEG1: 59, NBT_SEG2: 20, NBT_SJW: 2}
	if err := bad.ValidateTiming(); err == nil {
		t.Fatal("ValidateTiming() expected error for zero data timing")
	}
}

func TestCANFDInitConfigValidateWithBRP(t *testing.T) {
	ok := CANFDInitConfig{
		NBT_BRP: 1, NBT_SEG1: 59, NBT_SEG2: 20, NBT_SJW: 2,
		DBT_BRP: 1, DBT_SEG1: 14, DBT_SEG2: 5, DBT_SJW: 2,
	}
	if err := ok.ValidateWithBRP(); err != nil {
		t.Fatalf("ValidateWithBRP() unexpected error: %v", err)
	}

	noBRP := ok
	noBRP.NBT_BRP = 0
	if err := noBRP.ValidateWithBRP(); err == nil {
		t.Fatal("ValidateWithBRP() expected error for zero BRP")
	}
}
