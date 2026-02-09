package tp_layer

import "fmt"

// AddressingMode 定义了ISOTP支持的寻址模式
type AddressingMode int

const (
	Normal11Bit      AddressingMode = iota // 11位ID，无地址扩展
	Normal29Bit                            // 29位ID，无地址扩展
	NormalFixed29Bit                       // 29位ID，目标/源地址在ID中
	Extended11Bit                          // 11位ID，目标地址在数据负载第一字节
	Extended29Bit                          // 29位ID，目标地址在数据负载第一字节
	Mixed11Bit                             // 11位ID，地址扩展在数据负载第一字节
	Mixed29Bit                             // 29位ID，目标/源地址在ID中，地址扩展在数据负载第一字节
)

// AddressType 定义了寻址类型：物理或功能
type AddressType int

const (
	Physical AddressType = iota
	Functional
)

// Address 存储了所有与寻址相关的信息
type Address struct {
	AddressingMode   AddressingMode
	TxID             uint32
	RxID             uint32
	TargetAddress    byte
	SourceAddress    byte
	AddressExtension byte
	TxPayloadPrefix  []byte
	RxPrefixSize     int
	is29Bit          bool
}

// NewAddress 是一个灵活的构造函数，用于创建地址对象
func NewAddress(mode AddressingMode, opts ...func(*Address)) (*Address, error) {
	addr := &Address{AddressingMode: mode}

	// 应用所有可选配置
	for _, opt := range opts {
		opt(addr)
	}

	// 根据模式，自动计算和校验关键字段
	switch mode {
	case Normal11Bit:
		addr.is29Bit = false
	case Normal29Bit:
		addr.is29Bit = true
	case NormalFixed29Bit:
		addr.is29Bit = true
	case Extended11Bit:
		addr.is29Bit = false
		addr.TxPayloadPrefix = []byte{addr.TargetAddress}
		addr.RxPrefixSize = 1
	case Extended29Bit:
		addr.is29Bit = true
		addr.TxPayloadPrefix = []byte{addr.TargetAddress}
		addr.RxPrefixSize = 1
	case Mixed11Bit:
		// Not typically used, but included for completeness
		addr.is29Bit = false
		addr.TxPayloadPrefix = []byte{addr.AddressExtension}
		addr.RxPrefixSize = 1
	case Mixed29Bit:
		addr.is29Bit = true
		addr.TxPayloadPrefix = []byte{addr.AddressExtension}
		addr.RxPrefixSize = 1
	default:
		return nil, fmt.Errorf("不支持的寻址模式: %d", mode)
	}

	return addr, nil
}

// 可选配置函数，用于 NewAddress

func WithTxID(id uint32) func(*Address)        { return func(a *Address) { a.TxID = id } }
func WithRxID(id uint32) func(*Address)        { return func(a *Address) { a.RxID = id } }
func WithTargetAddress(ta byte) func(*Address) { return func(a *Address) { a.TargetAddress = ta } }
func WithSourceAddress(sa byte) func(*Address) { return func(a *Address) { a.SourceAddress = sa } }
func WithAddressExtension(ae byte) func(*Address) {
	return func(a *Address) { a.AddressExtension = ae }
}

// GetTxArbitrationID 根据寻址模式和类型（物理/功能）动态计算发送ID
func (a *Address) GetTxArbitrationID(addrType AddressType) uint32 {
	switch a.AddressingMode {
	case Normal11Bit, Normal29Bit, Extended11Bit, Extended29Bit, Mixed11Bit:
		return a.TxID
	case NormalFixed29Bit:
		prefix := uint32(0x18DA0000)
		if addrType == Functional {
			prefix = 0x18DB0000
		}
		return prefix | (uint32(a.TargetAddress) << 8) | uint32(a.SourceAddress)
	case Mixed29Bit:
		prefix := uint32(0x18CE0000)
		if addrType == Functional {
			prefix = 0x18CD0000
		}
		return prefix | (uint32(a.TargetAddress) << 8) | uint32(a.SourceAddress)
	}
	return a.TxID // Fallback
}

// IsForMe 检查收到的CAN报文是否是发给本ECU的
func (a *Address) IsForMe(msg *CanMessage) bool {
	if msg.IsExtendedID != a.is29Bit {
		return false // 11/29位不匹配
	}

	switch a.AddressingMode {
	case Normal11Bit, Normal29Bit:
		return msg.ArbitrationID == a.RxID
	case NormalFixed29Bit:
		return (msg.ArbitrationID & 0xFFFF0000) == (a.GetTxArbitrationID(Physical) & 0xFFFF0000)
	case Extended11Bit, Extended29Bit:
		if msg.ArbitrationID != a.RxID {
			return false
		}
		if len(msg.Data) < 1 {
			return false // No address extension byte
		}
		return msg.Data[0] == a.SourceAddress
	case Mixed29Bit:
		// A combination of NormalFixed and Extended checks
		if (msg.ArbitrationID & 0xFFFF0000) != (a.GetTxArbitrationID(Physical) & 0xFFFF0000) {
			return false
		}
		if len(msg.Data) < 1 {
			return false
		}
		return msg.Data[0] == a.AddressExtension
	}
	return false
}

// Is29Bit 返回当前模式是否为29位
func (a *Address) Is29Bit() bool {
	return a.is29Bit
}
