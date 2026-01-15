package tp_layer

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// pciTypeSingleFrame (SF) 是 0
	pciTypeSingleFrame = 0x00
	// pciTypeFirstFrame (FF) 是 1
	pciTypeFirstFrame = 0x10
	// pciTypeConsecutiveFrame (CF) 是 2
	pciTypeConsecutiveFrame = 0x20
	// pciTypeFlowControl (FC) 是 3
	pciTypeFlowControl = 0x30
)

// createFlowControlPayload 创建流控帧的数据负载
func createFlowControlPayload(status FlowStatus, blockSize int, stMinMs int) []byte {
	// 将 stMinMs 编码为字节
	var stMinByte byte
	if stMinMs >= 0 && stMinMs <= 127 {
		stMinByte = byte(stMinMs)
	} else {
		// 这里可以添加对微秒的编码，暂时简化
		stMinByte = 0x7F // 默认最大
	}

	return []byte{
		pciTypeFlowControl | byte(status),
		byte(blockSize),
		stMinByte,
	}
}

// createSingleFramePayload 创建单帧的数据负载
func createSingleFramePayload(data []byte, maxDataLength int) ([]byte, error) {
	dataLen := len(data)
	var pci []byte

	// 根据数据长度决定PCI格式
	if dataLen <= 7 {
		pci = []byte{pciTypeSingleFrame | byte(dataLen)}
	} else {
		// CAN FD 使用长度转义
		pci = []byte{pciTypeSingleFrame, byte(dataLen)}
	}

	totalLength := len(pci) + dataLen
	if totalLength > maxDataLength {
		return nil, fmt.Errorf("单帧总长度 (%d) 超过最大限制 (%d)", totalLength, maxDataLength)
	}

	payload := make([]byte, 0, totalLength)
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

	payload := make([]byte, 0, totalLength)
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
