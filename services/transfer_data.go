package services

import (
	"errors"
	"fmt"
	"time"

	"github.com/LoveWonYoung/canbuskit/uds_client"
)

const (
	transferDataSID      = 0x36
	transferDataPositive = 0x76
)

type TransferDataResponse struct {
	SequenceNumber  byte
	ParameterRecord []byte
	Raw             []byte
}

type TransferData struct {
	client  *uds_client.UDSClient
	timeout time.Duration
}

func NewTransferData(client *uds_client.UDSClient) *TransferData {
	return &TransferData{client: client}
}

func (t *TransferData) SetTimeout(timeout time.Duration) {
	if t == nil {
		return
	}
	t.timeout = timeout
}

func (t *TransferData) TransferData(sequenceNumber byte, data []byte) (*TransferDataResponse, error) {
	if t == nil || t.client == nil {
		return nil, errors.New("uds client is nil")
	}

	req := make([]byte, 0, 2+len(data))
	req = append(req, transferDataSID, sequenceNumber)
	if len(data) > 0 {
		req = append(req, data...)
	}

	resp, err := t.client.SendAndRecv(req, resolveTimeout(t.timeout))
	if err != nil {
		return nil, err
	}
	return parseTransferDataResponse(resp, sequenceNumber)
}

func (t *TransferData) TransferBlocks(blocks [][]byte, startSeq byte) (*TransferDataResponse, byte, error) {
	if t == nil || t.client == nil {
		return nil, startSeq, errors.New("uds client is nil")
	}
	if len(blocks) == 0 {
		return nil, startSeq, errors.New("no blocks to transfer")
	}
	if startSeq == 0 {
		return nil, startSeq, errors.New("start sequence number must be 1..0xFF")
	}
	if int(startSeq)+len(blocks)-1 > 0xFF {
		return nil, startSeq, fmt.Errorf("block count overflows sequence counter: start 0x%02X blocks %d", startSeq, len(blocks))
	}

	seq := startSeq
	var last *TransferDataResponse
	for i, block := range blocks {
		resp, err := t.TransferData(seq, block)
		if err != nil {
			return nil, seq, fmt.Errorf("block %d: %w", i, err)
		}
		last = resp
		seq++
	}

	return last, seq, nil
}

func (t *TransferData) TransferHexFile(filepath string, blockSize int, startSeq byte) (*TransferDataResponse, byte, error) {
	segments, err := ParseHexSegments(filepath, blockSize)
	if err != nil {
		return nil, startSeq, err
	}

	seq := startSeq
	var last *TransferDataResponse
	for i, segment := range segments {
		if len(segment.Blocks) == 0 {
			continue
		}
		resp, nextSeq, err := t.TransferBlocks(segment.Blocks, seq)
		if err != nil {
			return nil, seq, fmt.Errorf("segment %d: %w", i, err)
		}
		last = resp
		seq = nextSeq
	}

	if last == nil {
		return nil, startSeq, errors.New("no blocks to transfer")
	}
	return last, seq, nil
}

func parseTransferDataResponse(resp []byte, expectedSeq byte) (*TransferDataResponse, error) {
	if err := validateResponseSID(resp, transferDataPositive); err != nil {
		return nil, err
	}
	if len(resp) < 2 {
		return nil, fmt.Errorf("response data too short: % X", resp)
	}

	seq := resp[1]
	if seq != expectedSeq {
		return nil, fmt.Errorf("sequence mismatch: got 0x%02X want 0x%02X", seq, expectedSeq)
	}
	param := resp[2:]

	return &TransferDataResponse{
		SequenceNumber:  seq,
		ParameterRecord: param,
		Raw:             resp,
	}, nil
}
