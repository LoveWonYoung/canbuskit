package preset

import (
	"fmt"
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
}

func NewPresetToomoss(physId, respId, funcId uint32, channel byte, canType driver.CanType) (*Preset, error) {
	drv := driver.NewToomoss(canType, channel)
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

func (p *Preset) Write(id int32, fd bool, data []byte) error {
	return p.CanDevice.Write(id, fd, data)
}
