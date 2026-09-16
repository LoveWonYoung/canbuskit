package preset

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LoveWonYoung/canbuskit/driver"
	"github.com/LoveWonYoung/canbuskit/tp_layer"
	"github.com/LoveWonYoung/canbuskit/uds_client"
)

const defaultPaddingByte byte = 0xAA

var ErrUDSClientDisabled = errors.New("preset UDS client is disabled")

type options struct {
	enableUDSClient bool
	tpPadding       bool
	tpPaddingByte   byte
}

// Option configures a Preset.
type Option func(*options)

// WithUDSClient controls whether the preset creates and registers a UDS client.
// It is disabled by default, leaving the preset in raw CAN send/receive mode.
func WithUDSClient(enabled bool) Option {
	return func(opts *options) {
		opts.enableUDSClient = enabled
	}
}

// WithTPPadding controls whether the manual WriteTP* methods pad frames shorter
// than 8 bytes. Padding is disabled by default and uses 0xAA when enabled.
func WithTPPadding(enabled bool) Option {
	return func(opts *options) {
		opts.tpPadding = enabled
	}
}

// WithTPPaddingByte configures the byte used by manual TP frame padding.
// It does not enable padding by itself; combine it with WithTPPadding(true).
func WithTPPaddingByte(paddingByte byte) Option {
	return func(opts *options) {
		opts.tpPaddingByte = paddingByte
	}
}

type Preset struct {
	PhysId    uint32
	RespId    uint32
	FuncId    uint32
	CanDevice driver.CANDriver
	Client    *uds_client.UDSClient
	readMu    sync.Mutex
	rxChan    <-chan driver.CanFrame
	closeOnce sync.Once
	tpMu      sync.RWMutex
	tpPadding bool
	tpPadByte byte
}

func newPreset(drv driver.CANDriver, physId, respId, funcId uint32, presetOptions ...Option) (*Preset, error) {
	if drv == nil {
		return nil, errors.New("CAN driver instance cannot be nil")
	}

	opts := options{tpPaddingByte: defaultPaddingByte}
	for _, apply := range presetOptions {
		if apply != nil {
			apply(&opts)
		}
	}

	preset := &Preset{
		PhysId:    physId,
		RespId:    respId,
		FuncId:    funcId,
		CanDevice: drv,
		tpPadding: opts.tpPadding,
		tpPadByte: opts.tpPaddingByte,
	}
	if !opts.enableUDSClient {
		if err := startCANDevice(drv); err != nil {
			return nil, err
		}
		return preset, nil
	}

	physAddr, err := tp_layer.NewAddress(physId, respId)
	if err != nil {
		return nil, fmt.Errorf("build physical address: %w", err)
	}

	funcAddr, err := tp_layer.NewAddress(funcId, respId)
	if err != nil {
		return nil, fmt.Errorf("build functional address: %w", err)
	}

	pad := defaultPaddingByte
	cfg := tp_layer.DefaultConfig()
	cfg.PaddingByte = &pad

	client, err := uds_client.NewUDSClient(drv, physAddr, cfg)
	if err != nil {
		return nil, fmt.Errorf("initialize UDS client: %w", err)
	}
	if err := client.SetFunctionalAddress(funcAddr); err != nil {
		client.Close()
		return nil, fmt.Errorf("set functional address: %w", err)
	}
	preset.Client = client
	return preset, nil
}

func startCANDevice(drv driver.CANDriver) error {
	if err := drv.Init(); err != nil {
		return fmt.Errorf("initialize CAN device: %w", err)
	}
	if starter, ok := drv.(driver.ErrorStartingCANDriver); ok {
		if err := starter.StartWithError(); err != nil {
			drv.Stop()
			return fmt.Errorf("start CAN device: %w", err)
		}
		return nil
	}
	drv.Start()
	return nil
}

func (p *Preset) Close() {
	if p == nil {
		return
	}
	p.closeOnce.Do(func() {
		if p.Client != nil {
			p.Client.Close()
			return
		}
		if p.CanDevice != nil {
			p.CanDevice.Stop()
		}
	})
}

func (p *Preset) Request(payload []byte, timeout time.Duration) ([]byte, error) {
	if p == nil || p.Client == nil {
		return nil, ErrUDSClientDisabled
	}
	return p.Client.SendAndRecv(payload, timeout)
}

func (p *Preset) FunctionRequest(payload []byte, timeout time.Duration) ([]byte, error) {
	if p == nil || p.Client == nil {
		return nil, ErrUDSClientDisabled
	}
	return p.Client.SendAndRecvWithAddressingMode(payload, timeout, uds_client.AddressFunctional)
}

func (p *Preset) Write(id int32, fd bool, data []byte) error {
	if p == nil || p.CanDevice == nil {
		return errors.New("preset CAN device is not initialized")
	}
	return p.CanDevice.Write(id, fd, data)
}

// SetTPPadding enables or disables padding for the manual WriteTP* methods.
// When enabled, encoded TP frames shorter than 8 bytes are padded to 8 bytes.
func (p *Preset) SetTPPadding(enabled bool) {
	if p == nil {
		return
	}
	p.tpMu.Lock()
	p.tpPadding = enabled
	p.tpMu.Unlock()
}

// SetTPPaddingByte changes the byte used by manual TP frame padding.
func (p *Preset) SetTPPaddingByte(paddingByte byte) {
	if p == nil {
		return
	}
	p.tpMu.Lock()
	p.tpPadByte = paddingByte
	p.tpMu.Unlock()
}

// TPPadding returns the current manual TP padding settings.
func (p *Preset) TPPadding() (enabled bool, paddingByte byte) {
	if p == nil {
		return false, defaultPaddingByte
	}
	p.tpMu.RLock()
	defer p.tpMu.RUnlock()
	return p.tpPadding, p.tpPadByte
}

func (p *Preset) writeTPFrame(id int32, fd bool, frame []byte) error {
	enabled, paddingByte := p.TPPadding()
	if enabled && len(frame) < 8 {
		padded := make([]byte, 8)
		copy(padded, frame)
		for i := len(frame); i < len(padded); i++ {
			padded[i] = paddingByte
		}
		frame = padded
	}
	return p.Write(id, fd, frame)
}

// WriteTPSingleFrame encodes and writes one ISO-TP Single Frame. It does not
// require or use the preset UDS client and applies the configured TP padding.
func (p *Preset) WriteTPSingleFrame(id int32, fd bool, data []byte) error {
	frame, err := tp_layer.CreateSingleFrame(data, fd)
	if err != nil {
		return err
	}
	return p.writeTPFrame(id, fd, frame)
}

// WriteTPFirstFrame encodes and writes one ISO-TP First Frame. firstChunk is
// the payload carried by this frame and totalMessageSize is the complete
// ISO-TP message length.
func (p *Preset) WriteTPFirstFrame(id int32, fd bool, firstChunk []byte, totalMessageSize int) error {
	frame, err := tp_layer.CreateFirstFrame(firstChunk, totalMessageSize, fd)
	if err != nil {
		return err
	}
	return p.writeTPFrame(id, fd, frame)
}

// WriteTPFlowControlFrame encodes and writes one ISO-TP Flow Control Frame.
// stMin is the raw ISO-TP STmin byte.
func (p *Preset) WriteTPFlowControlFrame(id int32, fd bool, status tp_layer.FlowStatus, blockSize int, stMin byte) error {
	frame, err := tp_layer.CreateFlowControlFrame(status, blockSize, stMin)
	if err != nil {
		return err
	}
	return p.writeTPFrame(id, fd, frame)
}

// WriteTPConsecutiveFrame encodes and writes one ISO-TP Consecutive Frame.
// sequenceNumber must be in the range 0-15.
func (p *Preset) WriteTPConsecutiveFrame(id int32, fd bool, dataChunk []byte, sequenceNumber int) error {
	frame, err := tp_layer.CreateConsecutiveFrame(dataChunk, sequenceNumber, fd)
	if err != nil {
		return err
	}
	return p.writeTPFrame(id, fd, frame)
}

// SetDefaultStMin sets the STmin value advertised by automatic ISO-TP
// flow-control frames.
func (p *Preset) SetDefaultStMin(stMin int) error {
	if p == nil || p.Client == nil {
		return ErrUDSClientDisabled
	}
	return p.Client.SetDefaultStMin(stMin)
}

// SetDefaultBlockSize sets the block size advertised by automatic ISO-TP
// flow-control frames.
func (p *Preset) SetDefaultBlockSize(blockSize int) error {
	if p == nil || p.Client == nil {
		return ErrUDSClientDisabled
	}
	return p.Client.SetDefaultBlockSize(blockSize)
}

// SetManualFlowControl disables automatic ISO-TP flow-control transmission
// when enabled. The caller must send flow-control frames through Write.
func (p *Preset) SetManualFlowControl(enabled bool) {
	if p == nil || p.Client == nil {
		return
	}
	p.Client.SetManualFlowControl(enabled)
}

func (p *Preset) SetBRS(enabled bool) {
	if p == nil || p.CanDevice == nil {
		return
	}
	if ctl, ok := p.CanDevice.(driver.BRSController); ok {
		ctl.SetBRS(enabled)
	}
}

func (p *Preset) BRS() bool {
	if p == nil || p.CanDevice == nil {
		return false
	}
	if ctl, ok := p.CanDevice.(driver.BRSController); ok {
		return ctl.BRS()
	}
	return false
}

func (p *Preset) Read() <-chan driver.CanFrame {
	if p == nil || p.CanDevice == nil {
		return nil
	}

	p.readMu.Lock()
	defer p.readMu.Unlock()

	if p.rxChan == nil {
		p.rxChan = p.CanDevice.RxChan()
	}
	return p.rxChan
}
