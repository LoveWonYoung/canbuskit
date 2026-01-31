package services

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strings"
)

var (
	ErrChecksumMismatch = errors.New("checksum mismatch")
	ErrInvalidRecord    = errors.New("invalid record")
	ErrOverlap          = errors.New("overlapping segments")
)

type ParseError struct {
	Line int
	Err  error
}

func (e *ParseError) Error() string {
	return fmt.Sprintf("line %d: %v", e.Line, e.Err)
}

func (e *ParseError) Unwrap() error {
	return e.Err
}

type Segment struct {
	Address uint32
	Data    []byte
}

func (s Segment) End() uint32 {
	return s.Address + uint32(len(s.Data))
}

type File struct {
	Segments              []Segment
	Header                []byte
	ExecutionStartAddress *uint32
}

func ParseSrec(records string) (*File, error) {
	f := &File{}
	lines := strings.Split(records, "\n")

	for i, line := range lines {
		record := strings.TrimSpace(line)
		if record == "" {
			continue
		}

		recType, address, data, err := parseSrecRecord(record)
		if err != nil {
			return nil, &ParseError{Line: i + 1, Err: err}
		}

		switch recType {
		case '0':
			f.Header = append([]byte(nil), data...)
		case '1', '2', '3':
			f.Segments = append(f.Segments, Segment{
				Address: address,
				Data:    append([]byte(nil), data...),
			})
		case '5', '6':
			// Record count, ignore.
		case '7', '8', '9':
			addr := address
			f.ExecutionStartAddress = &addr
		default:
			return nil, &ParseError{Line: i + 1, Err: fmt.Errorf("%w: unsupported SREC type %q", ErrInvalidRecord, recType)}
		}
	}

	segments, err := normalizeSegments(f.Segments, false)
	if err != nil {
		return nil, err
	}
	f.Segments = segments

	return f, nil
}

func ParseS19(records string) (*File, error) {
	return ParseSrec(records)
}

func ParseIhex(records string) (*File, error) {
	f := &File{}
	var extendedSegmentAddress uint32
	var extendedLinearAddress uint32

	lines := strings.Split(records, "\n")

	for i, line := range lines {
		record := strings.TrimSpace(line)
		if record == "" {
			continue
		}

		recType, address, data, err := parseIhexRecord(record)
		if err != nil {
			return nil, &ParseError{Line: i + 1, Err: err}
		}

		switch recType {
		case 0x00:
			addr := uint64(address) + uint64(extendedSegmentAddress) + uint64(extendedLinearAddress)
			if addr > uint64(^uint32(0)) {
				return nil, &ParseError{Line: i + 1, Err: fmt.Errorf("%w: address overflow", ErrInvalidRecord)}
			}

			f.Segments = append(f.Segments, Segment{
				Address: uint32(addr),
				Data:    append([]byte(nil), data...),
			})
		case 0x01:
			// End of file.
		case 0x02:
			if len(data) != 2 {
				return nil, &ParseError{Line: i + 1, Err: fmt.Errorf("%w: bad extended segment address length", ErrInvalidRecord)}
			}
			extendedSegmentAddress = uint32(binary.BigEndian.Uint16(data)) * 16
		case 0x03:
			if len(data) != 4 {
				return nil, &ParseError{Line: i + 1, Err: fmt.Errorf("%w: bad start segment address length", ErrInvalidRecord)}
			}
			addr := binary.BigEndian.Uint32(data)
			f.ExecutionStartAddress = &addr
		case 0x04:
			if len(data) != 2 {
				return nil, &ParseError{Line: i + 1, Err: fmt.Errorf("%w: bad extended linear address length", ErrInvalidRecord)}
			}
			extendedLinearAddress = uint32(binary.BigEndian.Uint16(data)) << 16
		case 0x05:
			if len(data) != 4 {
				return nil, &ParseError{Line: i + 1, Err: fmt.Errorf("%w: bad start linear address length", ErrInvalidRecord)}
			}
			addr := binary.BigEndian.Uint32(data)
			f.ExecutionStartAddress = &addr
		default:
			return nil, &ParseError{Line: i + 1, Err: fmt.Errorf("%w: unsupported IHEX type 0x%02X", ErrInvalidRecord, recType)}
		}
	}

	segments, err := normalizeSegments(f.Segments, false)
	if err != nil {
		return nil, err
	}
	f.Segments = segments

	return f, nil
}

func ParseHex(records string) (*File, error) {
	return ParseIhex(records)
}

func parseSrecRecord(record string) (byte, uint32, []byte, error) {
	if len(record) < 6 {
		return 0, 0, nil, fmt.Errorf("%w: record too short", ErrInvalidRecord)
	}
	if record[0] != 'S' {
		return 0, 0, nil, fmt.Errorf("%w: missing 'S' prefix", ErrInvalidRecord)
	}

	value, err := decodeHex(record[2:])
	if err != nil {
		return 0, 0, nil, err
	}
	if len(value) < 2 {
		return 0, 0, nil, fmt.Errorf("%w: record too short", ErrInvalidRecord)
	}

	size := int(value[0])
	if size != len(value)-1 {
		return 0, 0, nil, fmt.Errorf("%w: wrong size", ErrInvalidRecord)
	}

	recType := record[1]
	width, err := srecAddressWidth(recType)
	if err != nil {
		return 0, 0, nil, err
	}
	if len(value) < 1+width+1 {
		return 0, 0, nil, fmt.Errorf("%w: record too short for address", ErrInvalidRecord)
	}

	address := uint32(0)
	for _, b := range value[1 : 1+width] {
		address = (address << 8) | uint32(b)
	}

	data := value[1+width : len(value)-1]
	expected := crcSrec(value[:len(value)-1])
	actual := value[len(value)-1]
	if actual != expected {
		return 0, 0, nil, fmt.Errorf("%w: expected 0x%02X, got 0x%02X", ErrChecksumMismatch, expected, actual)
	}

	return recType, address, data, nil
}

func parseIhexRecord(record string) (byte, uint16, []byte, error) {
	if len(record) < 11 {
		return 0, 0, nil, fmt.Errorf("%w: record too short", ErrInvalidRecord)
	}
	if record[0] != ':' {
		return 0, 0, nil, fmt.Errorf("%w: missing ':' prefix", ErrInvalidRecord)
	}

	value, err := decodeHex(record[1:])
	if err != nil {
		return 0, 0, nil, err
	}
	if len(value) < 5 {
		return 0, 0, nil, fmt.Errorf("%w: record too short", ErrInvalidRecord)
	}

	size := int(value[0])
	if size != len(value)-5 {
		return 0, 0, nil, fmt.Errorf("%w: wrong size", ErrInvalidRecord)
	}

	address := binary.BigEndian.Uint16(value[1:3])
	recType := value[3]
	data := value[4 : len(value)-1]
	expected := crcIhex(value[:len(value)-1])
	actual := value[len(value)-1]

	if actual != expected {
		return 0, 0, nil, fmt.Errorf("%w: expected 0x%02X, got 0x%02X", ErrChecksumMismatch, expected, actual)
	}

	return recType, address, data, nil
}

func srecAddressWidth(recType byte) (int, error) {
	switch recType {
	case '0', '1', '5', '9':
		return 2, nil
	case '2', '6', '8':
		return 3, nil
	case '3', '7':
		return 4, nil
	default:
		return 0, fmt.Errorf("%w: unsupported SREC type %q", ErrInvalidRecord, recType)
	}
}

func crcSrec(data []byte) byte {
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	return ^byte(sum & 0xFF)
}

func crcIhex(data []byte) byte {
	sum := 0
	for _, b := range data {
		sum += int(b)
	}
	return byte((^sum + 1) & 0xFF)
}

func decodeHex(s string) ([]byte, error) {
	if len(s)%2 != 0 {
		return nil, fmt.Errorf("%w: odd hex length", ErrInvalidRecord)
	}
	data, err := hex.DecodeString(s)
	if err != nil {
		return nil, fmt.Errorf("%w: bad hex data", ErrInvalidRecord)
	}
	return data, nil
}

// normalizeSegments sorts and merges adjacent segments, returning an error on overlap.
func normalizeSegments(segments []Segment, overwrite bool) ([]Segment, error) {
	if len(segments) == 0 {
		return nil, nil
	}

	sort.Slice(segments, func(i, j int) bool {
		return segments[i].Address < segments[j].Address
	})

	merged := make([]Segment, 0, len(segments))
	for _, seg := range segments {
		if len(seg.Data) == 0 {
			continue
		}

		segData := append([]byte(nil), seg.Data...)
		seg = Segment{Address: seg.Address, Data: segData}

		if len(merged) == 0 {
			merged = append(merged, seg)
			continue
		}

		last := &merged[len(merged)-1]
		lastEnd := last.End()
		segEnd := seg.End()

		switch {
		case seg.Address > lastEnd:
			merged = append(merged, seg)
		case seg.Address == lastEnd:
			last.Data = append(last.Data, seg.Data...)
		default:
			if !overwrite {
				return nil, fmt.Errorf("%w at 0x%08X", ErrOverlap, seg.Address)
			}

			overlapOffset := int(seg.Address - last.Address)
			if segEnd <= lastEnd {
				copy(last.Data[overlapOffset:], seg.Data)
				continue
			}

			copy(last.Data[overlapOffset:], seg.Data[:int(lastEnd-seg.Address)])
			last.Data = append(last.Data, seg.Data[int(lastEnd-seg.Address):]...)
		}
	}

	return merged, nil
}
