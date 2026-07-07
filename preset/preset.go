package preset

import (
	"fmt"
	"sync"
	"time"

	"github.com/LoveWonYoung/canbuskit/driver"
	"github.com/LoveWonYoung/canbuskit/tp_layer"
	"github.com/LoveWonYoung/canbuskit/uds_client"
)

const defaultPaddingByte byte = 0xAA

type Preset struct {
	PhysId    uint32
	RespId    uint32
	FuncId    uint32
	CanDevice driver.CANDriver
	Client    *uds_client.UDSClient
	readMu    sync.Mutex
	rxChan    <-chan driver.UnifiedCANMessage
}

func NewPresetToomoss(physId, respId, funcId uint32, channel byte, canType driver.CanType) (*Preset, error) {
	drv := driver.NewToomoss(canType, channel)
	return newPreset(drv, physId, respId, funcId, canType == driver.CANFD)
}

func NewPresetTSMaster(physId, respId, funcId uint32, channel byte, canType driver.CanType) (*Preset, error) {
	drv := driver.NewTSMaster(canType, channel)
	return newPreset(drv, physId, respId, funcId, canType == driver.CANFD)
}

func NewPresetPCAN(physId, respId, funcId uint32, channel byte, canType driver.CanType) (*Preset, error) {
	drv := driver.NewPCAN(canType, channel)
	return newPreset(drv, physId, respId, funcId, canType == driver.CANFD)
}

func NewPresetVector(physId, respId, funcId uint32, channel byte, canType driver.CanType, deviceType int) (*Preset, error) {
	drv := driver.NewVector(canType, deviceType, int(channel))
	return newPreset(drv, physId, respId, funcId, canType == driver.CANFD)
}

func newPreset(drv driver.CANDriver, physId, respId, funcId uint32, fd bool) (*Preset, error) {
	physAddr, err := tp_layer.NewAddress(
		tp_layer.Normal11Bit,
		tp_layer.WithTxID(physId),
		tp_layer.WithRxID(respId),
	)
	if err != nil {
		return nil, fmt.Errorf("build physical address: %w", err)
	}

	funcAddr, err := tp_layer.NewAddress(
		tp_layer.Normal11Bit,
		tp_layer.WithTxID(funcId),
		tp_layer.WithRxID(respId),
	)
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
	if fd {
		client.SetFDMode(true)
	}

	return &Preset{
		PhysId:    physId,
		RespId:    respId,
		FuncId:    funcId,
		CanDevice: drv,
		Client:    client,
	}, nil
}

func (p *Preset) Close() {
	if p != nil && p.Client != nil {
		p.Client.Close()
	}
}

func (p *Preset) Request(payload []byte, timeout time.Duration) ([]byte, error) {
	return p.Client.SendAndRecv(payload, timeout)
}

func (p *Preset) FunctionRequest(payload []byte, timeout time.Duration) ([]byte, error) {
	return p.Client.SendAndRecvWithAddressingMode(payload, timeout, uds_client.AddressFunctional)
}

func (p *Preset) Write(id int32, fd bool, data []byte) error {
	return p.CanDevice.Write(id, fd, data)
}

func (p *Preset) Read() <-chan driver.UnifiedCANMessage {
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
