package tp_layer

import "fmt"

const maxStandardCANID uint32 = 0x7FF

// Address defines the standard 11-bit CAN IDs used by one ISO-TP connection.
//
// Lite intentionally supports normal addressing only. Functional requests use
// another Address with a different TxID and the same response RxID.
type Address struct {
	TxID uint32
	RxID uint32
}

// NewAddress creates a normal-addressing ISO-TP address pair.
func NewAddress(txID, rxID uint32) (*Address, error) {
	addr := &Address{TxID: txID, RxID: rxID}
	if err := addr.Validate(); err != nil {
		return nil, err
	}
	return addr, nil
}

// Validate checks that both IDs fit the Lite frame model.
func (a *Address) Validate() error {
	if a == nil {
		return fmt.Errorf("address cannot be nil")
	}
	if err := validateStandardCANID("TxID", a.TxID); err != nil {
		return err
	}
	return validateStandardCANID("RxID", a.RxID)
}

func validateStandardCANID(name string, id uint32) error {
	if id > maxStandardCANID {
		return fmt.Errorf("%s 0x%X exceeds the standard 11-bit CAN ID range", name, id)
	}
	return nil
}

// IsForMe reports whether msg belongs to this ISO-TP connection.
func (a *Address) IsForMe(msg *CanMessage) bool {
	return a != nil && msg != nil && msg.ArbitrationID == a.RxID
}
