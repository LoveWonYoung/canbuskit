package services

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/LoveWonYoung/canbuskit/uds_client"
)

const (
	routineControlSID      = 0x31
	routineControlPositive = 0x71
)

type RoutineControlResponse struct {
	ControlType  byte
	RoutineID    uint16
	StatusRecord []byte
	Raw          []byte
}

type RoutineControl struct {
	client  *uds_client.UDSClient
	timeout time.Duration
}

func NewRoutineControl(client *uds_client.UDSClient) *RoutineControl {
	return &RoutineControl{client: client}
}

func (r *RoutineControl) SetTimeout(timeout time.Duration) {
	if r == nil {
		return
	}
	r.timeout = timeout
}

func (r *RoutineControl) RoutineControl(controlType byte, routineID uint16, data []byte) (*RoutineControlResponse, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("uds client is nil")
	}
	if controlType > 0x7F {
		return nil, fmt.Errorf("control type out of range: 0x%02X", controlType)
	}

	req := make([]byte, 0, 4+len(data))
	req = append(req, routineControlSID, controlType)
	req = append(req, byte(routineID>>8), byte(routineID))
	if len(data) > 0 {
		req = append(req, data...)
	}

	resp, err := r.client.SendAndRecv(req, resolveTimeout(r.timeout))
	if err != nil {
		return nil, err
	}
	return parseRoutineControlResponse(resp, controlType, routineID)
}

func parseRoutineControlResponse(resp []byte, expectedControl byte, expectedID uint16) (*RoutineControlResponse, error) {
	if len(resp) < 2 {
		return nil, fmt.Errorf("short response: % X", resp)
	}
	if err := validateResponseSID(resp, routineControlPositive); err != nil {
		return nil, err
	}
	if len(resp) < 4 {
		return nil, fmt.Errorf("response data too short: % X", resp)
	}

	control := resp[1]
	routineID := binary.BigEndian.Uint16(resp[2:4])
	if control != expectedControl || routineID != expectedID {
		return nil, fmt.Errorf("unexpected response echo: % X", resp)
	}

	status := resp[4:]
	return &RoutineControlResponse{
		ControlType:  control,
		RoutineID:    routineID,
		StatusRecord: status,
		Raw:          resp,
	}, nil
}
