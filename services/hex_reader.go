package services

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

type HexSegment struct {
	Address uint32
	Data    []byte
	Blocks  [][]byte
}

func IntToBig(num uint32) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, num)
	return buf
}

func SplitBlock(data []byte, bs int) [][]byte {
	if bs <= 0 {
		return nil
	}
	chunkCount := (len(data) + bs - 1) / bs
	smallSlices := make([][]byte, 0, chunkCount)

	// 循环拆分大切片
	for i := 0; i < len(data); i += bs {
		end := i + bs
		// 如果最后一个切片长度不足 bs，则 end 设置为实际长度
		if end > len(data) {
			end = len(data)
		}
		// 将大切片中的一部分作为小切片添加到集合中
		smallSlices = append(smallSlices, data[i:end])
	}
	return smallSlices
}

func ParseHexSegments(filepath string, blockSize int) ([]HexSegment, error) {
	if blockSize <= 0 {
		return nil, fmt.Errorf("blockSize must be > 0")
	}

	raw, err := os.ReadFile(filepath)
	if err != nil {
		return nil, err
	}

	file, err := parseBincopyFile(filepath, string(raw))
	if err != nil {
		return nil, err
	}
	if len(file.Segments) == 0 {
		return nil, fmt.Errorf("no data segments found in file")
	}

	out := make([]HexSegment, 0, len(file.Segments))
	for _, seg := range file.Segments {
		if len(seg.Data) == 0 {
			continue
		}
		out = append(out, HexSegment{
			Address: seg.Address,
			Data:    seg.Data,
			Blocks:  SplitBlock(seg.Data, blockSize),
		})
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("no data segments found in file")
	}
	return out, nil
}

func parseBincopyFile(path string, content string) (*File, error) {
	ext := strings.ToLower(filepath.Ext(path))
	switch ext {
	case ".hex", ".ihex":
		return ParseIhex(content)
	case ".s19", ".srec", ".s28", ".s37", ".serc":
		return ParseSrec(content)
	default:
		return parseBincopyByContent(content)
	}
}

func parseBincopyByContent(content string) (*File, error) {
	for _, line := range strings.Split(content, "\n") {
		record := strings.TrimSpace(line)
		if record == "" {
			continue
		}
		record = strings.TrimPrefix(record, "\uFEFF")
		if record == "" {
			continue
		}
		switch record[0] {
		case ':':
			return ParseIhex(content)
		case 'S':
			return ParseSrec(content)
		default:
			return nil, fmt.Errorf("unsupported file format: first record starts with %q", record[0])
		}
	}
	return nil, fmt.Errorf("empty file")
}

func MyHexParser(
	filepath string,
	blockSize int) (dataLen []byte, startAddr []byte, data [][]byte, err error) {

	return MyHexParserWithLengths(filepath, blockSize, 4, 4)
}

func MyHexParserWithLengths(
	filepath string,
	blockSize int,
	addrLen int,
	sizeLen int) (dataLen []byte, startAddr []byte, data [][]byte, err error) {

	segments, err := ParseHexSegments(filepath, blockSize)
	if err != nil {
		return nil, nil, nil, err
	}
	if len(segments) != 1 {
		return nil, nil, nil, fmt.Errorf("expected single segment, got %d", len(segments))
	}
	segment := segments[0]

	startAddr, err = encodeUint(uint64(segment.Address), addrLen)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("address: %w", err)
	}
	dataLen, err = encodeUint(uint64(len(segment.Data)), sizeLen)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("size: %w", err)
	}
	data = segment.Blocks
	return
}

func MyHexParserFirstSegment(
	filepath string,
	blockSize int) (dataLen []byte, startAddr []byte, data [][]byte, err error) {

	segments, err := ParseHexSegments(filepath, blockSize)
	if err != nil {
		return nil, nil, nil, err
	}
	segment := segments[0]
	startAddr = IntToBig(segment.Address)
	dataLen = IntToBig(uint32(len(segment.Data)))
	data = segment.Blocks
	return
}
