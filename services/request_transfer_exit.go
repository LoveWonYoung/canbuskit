package services

import (
	"errors"
	"time"

	"github.com/LoveWonYoung/canbuskit/uds_client"
)

const (
	requestTransferExitSID      = 0x37
	requestTransferExitPositive = 0x77
)

type RequestTransferExitResponse struct {
	ParameterRecord []byte
	Raw             []byte
}

type RequestTransferExit struct {
	client  *uds_client.UDSClient
	timeout time.Duration
}

func NewRequestTransferExit(client *uds_client.UDSClient) *RequestTransferExit {
	return &RequestTransferExit{client: client}
}

func (r *RequestTransferExit) SetTimeout(timeout time.Duration) {
	if r == nil {
		return
	}
	r.timeout = timeout
}

func (r *RequestTransferExit) RequestTransferExit(data []byte) (*RequestTransferExitResponse, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("uds client is nil")
	}

	req := make([]byte, 0, 1+len(data))
	req = append(req, requestTransferExitSID)
	if len(data) > 0 {
		req = append(req, data...)
	}

	resp, err := r.client.SendAndRecv(req, resolveTimeout(r.timeout))
	if err != nil {
		return nil, err
	}
	return parseRequestTransferExitResponse(resp)
}

func parseRequestTransferExitResponse(resp []byte) (*RequestTransferExitResponse, error) {
	if err := validateResponseSID(resp, requestTransferExitPositive); err != nil {
		return nil, err
	}

	return &RequestTransferExitResponse{
		ParameterRecord: resp[1:],
		Raw:             resp,
	}, nil
}
