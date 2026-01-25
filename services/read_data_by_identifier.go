package services

import (
	"encoding/binary"
	"errors"
	"fmt"
	"time"

	"github.com/LoveWonYoung/canbuskit/uds_client"
)

const (
	readDataByIdentifierSID      = 0x22
	readDataByIdentifierPositive = 0x62
)

type ReadDataByIdentifierResponse struct {
	Values map[uint16][]byte
	Raw    []byte
}

type ReadDataByIdentifier struct {
	client  *uds_client.UDSClient
	timeout time.Duration
}

func NewReadDataByIdentifier(client *uds_client.UDSClient) *ReadDataByIdentifier {
	return &ReadDataByIdentifier{client: client}
}

func (r *ReadDataByIdentifier) SetTimeout(timeout time.Duration) {
	if r == nil {
		return
	}
	r.timeout = timeout
}

func (r *ReadDataByIdentifier) ReadDataByIdentifier(dids ...uint16) (*ReadDataByIdentifierResponse, error) {
	return r.ReadDataByIdentifierWithLengths(nil, dids...)
}

func (r *ReadDataByIdentifier) ReadDataByIdentifierWithLengths(lengths map[uint16]int, dids ...uint16) (*ReadDataByIdentifierResponse, error) {
	if r == nil || r.client == nil {
		return nil, errors.New("uds client is nil")
	}
	if len(dids) == 0 {
		return nil, errors.New("no data identifiers provided")
	}

	req := make([]byte, 0, 1+2*len(dids))
	req = append(req, readDataByIdentifierSID)
	for _, did := range dids {
		req = append(req, byte(did>>8), byte(did))
	}

	resp, err := r.client.SendAndRecv(req, resolveTimeout(r.timeout))
	if err != nil {
		return nil, err
	}
	return parseReadDataByIdentifierResponse(resp, dids, lengths)
}

func parseReadDataByIdentifierResponse(resp []byte, dids []uint16, lengths map[uint16]int) (*ReadDataByIdentifierResponse, error) {
	if err := validateResponseSID(resp, readDataByIdentifierPositive); err != nil {
		return nil, err
	}

	data := resp[1:]
	if len(data) < 2 {
		return nil, fmt.Errorf("response data too short: % X", resp)
	}

	values := make(map[uint16][]byte)
	if lengths == nil {
		if len(dids) != 1 {
			return nil, errors.New("lengths required when reading multiple DIDs")
		}
		did := binary.BigEndian.Uint16(data[:2])
		if did != dids[0] {
			return nil, fmt.Errorf("unexpected DID: 0x%04X", did)
		}
		values[did] = data[2:]
		return &ReadDataByIdentifierResponse{Values: values, Raw: resp}, nil
	}

	offset := 0
	for offset < len(data) {
		if len(data)-offset < 2 {
			if isZeroPadding(data[offset:]) {
				break
			}
			return nil, fmt.Errorf("response data incomplete: % X", resp)
		}
		did := binary.BigEndian.Uint16(data[offset : offset+2])
		if did == 0 && isZeroPadding(data[offset:]) {
			break
		}
		offset += 2

		length, ok := lengths[did]
		if !ok {
			return nil, fmt.Errorf("no length configured for DID 0x%04X", did)
		}
		if length < 0 {
			return nil, fmt.Errorf("invalid length for DID 0x%04X", did)
		}
		if len(data) < offset+length {
			return nil, fmt.Errorf("value for DID 0x%04X incomplete", did)
		}
		values[did] = data[offset : offset+length]
		offset += length
	}

	for _, did := range dids {
		if _, ok := values[did]; !ok {
			return nil, fmt.Errorf("missing DID 0x%04X in response", did)
		}
	}

	return &ReadDataByIdentifierResponse{Values: values, Raw: resp}, nil
}

func isZeroPadding(data []byte) bool {
	for _, b := range data {
		if b != 0x00 {
			return false
		}
	}
	return true
}
