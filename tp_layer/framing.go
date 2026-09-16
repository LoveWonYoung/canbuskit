package tp_layer

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	pciTypeSingleFrame      = 0x00
	pciTypeFirstFrame       = 0x10
	pciTypeConsecutiveFrame = 0x20
	pciTypeFlowControl      = 0x30
)

// CreateSingleFrame encodes one ISO-TP Single Frame without padding.
// Set isFD when the frame will be sent as CAN FD.
func CreateSingleFrame(data []byte, isFD bool) ([]byte, error) {
	return createSingleFramePayload(data, frameDataLength(isFD))
}

// CreateFirstFrame encodes one ISO-TP First Frame without padding.
// firstChunk is only the payload carried by this frame; totalMessageSize is
// the size of the complete ISO-TP message.
func CreateFirstFrame(firstChunk []byte, totalMessageSize int, isFD bool) ([]byte, error) {
	if totalMessageSize <= 0 {
		return nil, fmt.Errorf("消息总长度必须大于0: %d", totalMessageSize)
	}
	if uint64(totalMessageSize) > uint64(^uint32(0)) {
		return nil, fmt.Errorf("消息总长度超过ISO-TP 32位长度限制: %d", totalMessageSize)
	}
	if totalMessageSize <= len(firstChunk) {
		return nil, fmt.Errorf("消息总长度 (%d) 必须大于首帧数据长度 (%d)", totalMessageSize, len(firstChunk))
	}
	return createFirstFramePayload(firstChunk, totalMessageSize, frameDataLength(isFD))
}

// CreateConsecutiveFrame encodes one ISO-TP Consecutive Frame without padding.
// sequenceNumber must be in the range 0-15 and is normally started at 1.
func CreateConsecutiveFrame(dataChunk []byte, sequenceNumber int, isFD bool) ([]byte, error) {
	maxChunkLength := frameDataLength(isFD) - 1
	if len(dataChunk) > maxChunkLength {
		return nil, fmt.Errorf("连续帧数据长度 (%d) 超过最大限制 (%d)", len(dataChunk), maxChunkLength)
	}
	return createConsecutiveFramePayload(dataChunk, sequenceNumber)
}

// CreateFlowControlFrame encodes one ISO-TP Flow Control Frame without padding.
// stMin is the raw ISO-TP STmin byte: 0x00-0x7F represent milliseconds and
// 0xF1-0xF9 represent 100-900 microseconds.
func CreateFlowControlFrame(status FlowStatus, blockSize int, stMin byte) ([]byte, error) {
	if status > FlowStatusOverflow {
		return nil, fmt.Errorf("无效流控状态: 0x%X", status)
	}
	if blockSize < 0 || blockSize > 255 {
		return nil, fmt.Errorf("块大小必须在0到255之间: %d", blockSize)
	}
	if stMin > 0x7F && (stMin < 0xF1 || stMin > 0xF9) {
		return nil, fmt.Errorf("无效STmin编码: 0x%02X", stMin)
	}
	return createFlowControlPayloadRaw(status, byte(blockSize), stMin), nil
}

func frameDataLength(isFD bool) int {
	if isFD {
		return 64
	}
	return 8
}

// createFlowControlPayload 创建流控帧的数据负载
func createFlowControlPayload(status FlowStatus, blockSize int, stMinMs int) []byte {
	var stMinByte byte
	if stMinMs >= 0 && stMinMs <= 127 {
		stMinByte = byte(stMinMs)
	} else {
		stMinByte = 0x7F // 默认最大
	}
	return createFlowControlPayloadRaw(status, byte(blockSize), stMinByte)
}

func createFlowControlPayloadRaw(status FlowStatus, blockSize, stMin byte) []byte {
	return []byte{
		pciTypeFlowControl | byte(status),
		blockSize,
		stMin,
	}
}

// createSingleFramePayload 创建单帧的数据负载
func createSingleFramePayload(data []byte, maxDataLength int) ([]byte, error) {
	dataLen := len(data)
	var pci []byte
	if dataLen == 0 && maxDataLength > 8 {
		pci = []byte{pciTypeSingleFrame, 0}
	} else if dataLen <= 7 {
		pci = []byte{pciTypeSingleFrame | byte(dataLen)}
	} else {
		pci = []byte{pciTypeSingleFrame, byte(dataLen)}
	}

	totalLength := len(pci) + dataLen
	if totalLength > maxDataLength {
		return nil, fmt.Errorf("单帧总长度 (%d) 超过最大限制 (%d)", totalLength, maxDataLength)
	}

	payload := make([]byte, 0, maxDataLength)
	payload = append(payload, pci...)
	payload = append(payload, data...)
	return payload, nil
}

// createFirstFramePayload 创建首帧的数据负载
func createFirstFramePayload(firstChunk []byte, totalMessageSize int, maxDataLength int) ([]byte, error) {
	var pci []byte
	if totalMessageSize <= 4095 { // 12-bit length
		pci = []byte{
			pciTypeFirstFrame | byte(totalMessageSize>>8&0x0F),
			byte(totalMessageSize & 0xFF),
		}
	} else { // 32-bit length
		pci = make([]byte, 6)
		pci[0] = pciTypeFirstFrame
		pci[1] = 0x00
		binary.BigEndian.PutUint32(pci[2:], uint32(totalMessageSize))
	}

	totalLength := len(pci) + len(firstChunk)
	if totalLength > maxDataLength {
		return nil, fmt.Errorf("首帧总长度 (%d) 超过最大限制 (%d)", totalLength, maxDataLength)
	}

	payload := make([]byte, 0, maxDataLength)
	payload = append(payload, pci...)
	payload = append(payload, firstChunk...)
	return payload, nil
}

// createConsecutiveFramePayload 创建连续帧的数据负载
func createConsecutiveFramePayload(dataChunk []byte, sequenceNumber int) ([]byte, error) {
	if sequenceNumber < 0 || sequenceNumber > 15 {
		return nil, errors.New("序列号必须在0到15之间")
	}
	pci := []byte{pciTypeConsecutiveFrame | byte(sequenceNumber)}
	payload := make([]byte, 0, len(pci)+len(dataChunk))
	payload = append(payload, pci...)
	payload = append(payload, dataChunk...)
	return payload, nil
}
