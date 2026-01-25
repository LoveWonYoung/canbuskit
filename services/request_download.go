package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/LoveWonYoung/canbuskit/uds_client"
)

const (
	requestDownloadSID      = 0x34
	requestDownloadPositive = 0x74
)

type RequestDownloadResponse struct {
	MaxLength uint64
	Raw       []byte
}

type RequestDownload struct {
	client  *uds_client.UDSClient
	timeout time.Duration
}

func NewRequestDownload(client *uds_client.UDSClient) *RequestDownload {
	return &RequestDownload{client: client}
}

func (r *RequestDownload) SetTimeout(timeout time.Duration) {
	if r == nil {
		return
	}
	r.timeout = timeout
}

func (r *RequestDownload) RequestDownload(address, size uint64, addrLen, sizeLen int) (*RequestDownloadResponse, error) {
	return r.RequestDownloadWithFormat(address, size, addrLen, sizeLen, 0x00)
}

func (r *RequestDownload) RequestDownloadWithFormat(address, size uint64, addrLen, sizeLen int, dataFormat byte) (*RequestDownloadResponse, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("uds client is nil")
	}

	alfid, err := buildALFID(addrLen, sizeLen)
	if err != nil {
		return nil, err
	}
	addrBytes, err := encodeUint(address, addrLen)
	if err != nil {
		return nil, fmt.Errorf("address: %w", err)
	}
	sizeBytes, err := encodeUint(size, sizeLen)
	if err != nil {
		return nil, fmt.Errorf("size: %w", err)
	}

	req := make([]byte, 0, 3+len(addrBytes)+len(sizeBytes))
	req = append(req, requestDownloadSID, dataFormat)
	req = append(req, alfid)
	req = append(req, addrBytes...)
	req = append(req, sizeBytes...)

	resp, err := r.client.SendAndRecv(req, resolveTimeout(r.timeout))
	if err != nil {
		return nil, err
	}
	return parseRequestDownloadResponse(resp)
}

func parseRequestDownloadResponse(resp []byte) (*RequestDownloadResponse, error) {
	if len(resp) < 2 {
		return nil, fmt.Errorf("short response: % X", resp)
	}
	if err := validateResponseSID(resp, requestDownloadPositive); err != nil {
		return nil, err
	}

	data := resp[1:]
	lfid := int(data[0] >> 4)
	if lfid < 1 || lfid > 8 {
		return nil, fmt.Errorf("unsupported length size: %d", lfid)
	}
	if len(data) < lfid+1 {
		return nil, fmt.Errorf("response data too short: % X", resp)
	}

	var maxLen uint64
	for i := 1; i <= lfid; i++ {
		maxLen = (maxLen << 8) | uint64(data[i])
	}

	return &RequestDownloadResponse{
		MaxLength: maxLen,
		Raw:       resp,
	}, nil
}

func buildALFID(addrLen, sizeLen int) (byte, error) {
	if addrLen < 1 || addrLen > 8 {
		return 0, fmt.Errorf("address length must be 1..8, got %d", addrLen)
	}
	if sizeLen < 1 || sizeLen > 8 {
		return 0, fmt.Errorf("size length must be 1..8, got %d", sizeLen)
	}
	return byte((addrLen << 4) | sizeLen), nil
}

func encodeUint(value uint64, length int) ([]byte, error) {
	if length < 1 || length > 8 {
		return nil, fmt.Errorf("length must be 1..8, got %d", length)
	}
	if length < 8 && value>>(8*length) != 0 {
		return nil, fmt.Errorf("value 0x%X does not fit in %d bytes", value, length)
	}

	out := make([]byte, length)
	for i := length - 1; i >= 0; i-- {
		out[i] = byte(value)
		value >>= 8
	}
	return out, nil
}
