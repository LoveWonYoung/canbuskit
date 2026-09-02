package driver

import (
	"sync"
	"time"
)

const (
	defaultBusLoadWindow = time.Second
	busLoadSlotCount     = 20
	pendingTxTTL         = 100 * time.Millisecond
	maxPendingTx         = 256
)

// BusLoadInfo is a snapshot of estimated CAN bus occupancy over a recent window.
type BusLoadInfo struct {
	Load           float64
	Window         time.Duration
	NominalBitrate uint32
	DataBitrate    uint32
	FrameCount     uint64
}

type busLoadSlot struct {
	occupiedNs uint64
	frames     uint64
}

type pendingTx struct {
	key uint64
	at  time.Time
}

type busLoadMeter struct {
	mu             sync.Mutex
	nominalBitrate uint32
	dataBitrate    uint32
	window         time.Duration
	slot           time.Duration
	origin         time.Time
	started        time.Time
	slots          [busLoadSlotCount]busLoadSlot
	pending        []pendingTx
}

func (m *busLoadMeter) configure(cfg Config) {
	m.mu.Lock()
	defer m.mu.Unlock()
	nominal := cfg.NominalBitrate
	if nominal == 0 {
		nominal = 500_000
	}
	data := cfg.DataBitrate
	if data == 0 {
		data = 2_000_000
	}
	m.nominalBitrate = nominal
	m.dataBitrate = data
	m.window = defaultBusLoadWindow
	m.slot = defaultBusLoadWindow / busLoadSlotCount
	m.origin = time.Time{}
	m.started = time.Time{}
	m.slots = [busLoadSlotCount]busLoadSlot{}
	m.pending = nil
}

func (m *busLoadMeter) recordTx(id int32, fd, brs bool, data []byte, now time.Time) {
	frame := CanFrame{
		Direction: TX,
		ID:        uint32(id),
		DLC:       dataLenToDlc(len(data)),
		IsFD:      fd,
		BRS:       fd && brs,
	}
	copy(frame.Data[:], data)

	m.mu.Lock()
	defer m.mu.Unlock()
	if m.window == 0 {
		return
	}
	m.addLocked(frame, now)
	m.pushPending(frame, now)
}

func (m *busLoadMeter) observe(frame CanFrame, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.window == 0 {
		return
	}
	m.expirePending(now)
	if frame.Direction == TX {
		return
	}
	if m.consumePending(frame) {
		return
	}
	m.addLocked(frame, now)
}

func (m *busLoadMeter) snapshot(now time.Time) BusLoadInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.window == 0 || m.nominalBitrate == 0 {
		return BusLoadInfo{}
	}
	m.rotate(now)
	m.expirePending(now)

	var occupiedNs, frames uint64
	for i := range m.slots {
		occupiedNs += m.slots[i].occupiedNs
		frames += m.slots[i].frames
	}

	denom := m.window
	if m.started.IsZero() {
		return BusLoadInfo{
			Window:         m.window,
			NominalBitrate: m.nominalBitrate,
			DataBitrate:    m.dataBitrate,
		}
	}
	if age := now.Sub(m.started); age > 0 && age < m.window {
		denom = age
	}
	load := float64(occupiedNs) / float64(denom)
	if load > 1 {
		load = 1
	}
	return BusLoadInfo{
		Load:           load,
		Window:         denom,
		NominalBitrate: m.nominalBitrate,
		DataBitrate:    m.dataBitrate,
		FrameCount:     frames,
	}
}

func (m *busLoadMeter) addLocked(frame CanFrame, now time.Time) {
	occupied := frameOccupancy(frame, m.nominalBitrate, m.dataBitrate)
	if occupied <= 0 {
		return
	}
	m.rotate(now)
	idx := int(now.Sub(m.origin) / m.slot)
	if idx >= len(m.slots) {
		idx = len(m.slots) - 1
	}
	if idx < 0 {
		return
	}
	m.slots[idx].occupiedNs += uint64(occupied)
	m.slots[idx].frames++
}

func (m *busLoadMeter) rotate(now time.Time) {
	if m.origin.IsZero() {
		m.origin = now
		m.started = now
		return
	}
	if now.Before(m.origin) {
		return
	}
	elapsed := now.Sub(m.origin)
	if elapsed <= m.window {
		return
	}
	shift := int((elapsed - m.window + m.slot - 1) / m.slot)
	if shift < 1 {
		shift = 1
	}
	if shift >= len(m.slots) {
		for i := range m.slots {
			m.slots[i] = busLoadSlot{}
		}
		m.origin = now
		return
	}
	copy(m.slots[:], m.slots[shift:])
	for i := len(m.slots) - shift; i < len(m.slots); i++ {
		m.slots[i] = busLoadSlot{}
	}
	m.origin = m.origin.Add(time.Duration(shift) * m.slot)
}

func (m *busLoadMeter) pushPending(frame CanFrame, now time.Time) {
	m.expirePending(now)
	if len(m.pending) >= maxPendingTx {
		m.pending = m.pending[1:]
	}
	m.pending = append(m.pending, pendingTx{key: busFrameKey(frame), at: now})
}

func (m *busLoadMeter) consumePending(frame CanFrame) bool {
	key := busFrameKey(frame)
	for i, pending := range m.pending {
		if pending.key != key {
			continue
		}
		m.pending = append(m.pending[:i], m.pending[i+1:]...)
		return true
	}
	return false
}

func (m *busLoadMeter) expirePending(now time.Time) {
	kept := m.pending[:0]
	for _, pending := range m.pending {
		if now.Sub(pending.at) <= pendingTxTTL {
			kept = append(kept, pending)
		}
	}
	m.pending = kept
}

func busFrameKey(frame CanFrame) uint64 {
	n := frame.DataLength()
	if n > 64 {
		n = 64
	}
	var hash uint64
	for i := 0; i < n; i++ {
		hash = hash*131 + uint64(frame.Data[i])
	}
	key := uint64(frame.ID) & 0x7FF
	key |= uint64(frame.DLC) << 11
	if frame.IsFD {
		key |= 1 << 15
	}
	return key | hash<<16
}

func frameOccupancy(frame CanFrame, nominalBitrate, dataBitrate uint32) time.Duration {
	if nominalBitrate == 0 {
		return 0
	}
	arbBits, dataBits := frameBitCounts(frame)
	occupied := bitsToDuration(arbBits, nominalBitrate)
	if dataBits > 0 && dataBitrate > 0 {
		occupied += bitsToDuration(dataBits, dataBitrate)
	}
	return occupied
}

func frameBitCounts(frame CanFrame) (arbBits, dataBits int) {
	if frame.IsFD {
		return canFDFrameBits(frame)
	}
	return classicCANFrameBits(frame), 0
}

func bitsToDuration(bits int, bitrate uint32) time.Duration {
	if bits <= 0 || bitrate == 0 {
		return 0
	}
	return time.Duration(int64(bits) * int64(time.Second) / int64(bitrate))
}

// classicCANFrameBits returns on-wire bits for an 11-bit data frame, including
// stuff bits and the 3-bit intermission.
func classicCANFrameBits(frame CanFrame) int {
	payload := frame.DataLength()
	if payload > 8 {
		payload = 8
	}

	bits := make([]byte, 0, 34+8*payload)
	bits = appendBits(bits, 0, 1) // SOF
	bits = appendBits(bits, uint32(frame.ID), 11)
	bits = appendBits(bits, 0, 1) // RTR
	bits = appendBits(bits, 0, 1) // IDE
	bits = appendBits(bits, 0, 1) // r0
	bits = appendBits(bits, uint32(frame.DLC&0xF), 4)
	for i := 0; i < payload; i++ {
		bits = appendBits(bits, uint32(frame.Data[i]), 8)
	}
	crc := crc15CAN(bits)
	bits = appendBits(bits, uint32(crc), 15)

	return stuffedBitCount(bits) +
		1 + // CRC delimiter
		1 + // ACK
		1 + // ACK delimiter
		7 + // EOF
		3 // IFS
}

const (
	canFDSFFArbUnstuffed = 17 // SOF, ID, RRS, IDE, FDF, res, BRS
	canFDCrcDelimiter    = 1
	canFDAckTrailer      = 1 + 1 + 7 + 3 // ACK, ACK delimiter, EOF, IFS
)

// canFDFrameBits estimates on-wire bits for an 11-bit CAN-FD frame.
//
// CANoe bus statistics use worst-case stuffing on SOF through the CRC
// sequence (including DLC) and do not stuff CRC delimiter / ACK / EOF / IFS.
// That combination matches observed CANoe percentages much more closely than
// linux-can's shorter 5/4 estimate, which omitted DLC and stuffed the trailer.
func canFDFrameBits(frame CanFrame) (arbBits, dataBits int) {
	payload := frame.DataLength()
	if payload > 64 {
		payload = 64
	}
	crcLen := 17
	if payload > 16 {
		crcLen = 21
	}
	// SOF + ID + RRS/IDE/FDF/res/BRS + DLC + data + CRC
	stuffable := 1 + 11 + 5 + 4 + payload*8 + crcLen
	stuffed := stuffable * 5 / 4
	trailer := canFDCrcDelimiter + canFDAckTrailer
	if !frame.BRS {
		return stuffed + trailer, 0
	}
	arbStuffed := canFDSFFArbUnstuffed * 5 / 4
	return arbStuffed + canFDAckTrailer, stuffed - arbStuffed + canFDCrcDelimiter
}

func appendBits(dst []byte, value uint32, n int) []byte {
	for i := n - 1; i >= 0; i-- {
		dst = append(dst, byte((value>>i)&1))
	}
	return dst
}

func crc15CAN(bits []byte) uint16 {
	var crc uint16
	for _, bit := range bits {
		msb := byte((crc >> 14) & 1)
		crc = (crc << 1) & 0x7fff
		if msb^(bit&1) == 1 {
			crc ^= 0x4599
		}
	}
	return crc
}

func stuffedBitCount(bits []byte) int {
	if len(bits) == 0 {
		return 0
	}
	total := 0
	run := 0
	prev := byte(2)
	for _, bit := range bits {
		bit &= 1
		total++
		if bit == prev {
			run++
		} else {
			prev = bit
			run = 1
		}
		if run == 5 {
			total++
			prev ^= 1
			run = 1
		}
	}
	return total
}
