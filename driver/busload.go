package driver

import (
	"sync"
	"time"
)

const (
	defaultBusLoadWindow = time.Second
	busLoadSlotCount     = 1000                   // 1 ms buckets limit rolling-window edge error to 1 ms
	pendingTxTTL         = 100 * time.Millisecond // limit RX lookalike deduplication
	pendingTxEchoTTL     = time.Second            // allow delayed, explicitly marked TX confirmations
	maxPendingTx         = 256
)

// BusLoadInfo is a snapshot of estimated CAN bus occupancy over a recent window.
// Frames with device timestamps use device time for window placement; frames
// without one use the host monotonic clock.
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
	key        uint64
	at         time.Time
	occupiedNs uint64
	confirmed  bool
}

type earlyTxEcho struct {
	frame        CanFrame
	at           time.Time
	wrapPeriodUS uint64
	occupiedNs   uint64
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
	hwSlots        [busLoadSlotCount]busLoadSlot
	hwOriginUS     uint64
	hwLastRawUS    uint64
	hwLastUS       uint64
	hwFirstUS      uint64
	hwLastHost     time.Time
	hwInitialized  bool
	pending        []pendingTx
	earlyTx        []earlyTxEcho
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
	m.hwSlots = [busLoadSlotCount]busLoadSlot{}
	m.hwOriginUS = 0
	m.hwLastRawUS = 0
	m.hwLastUS = 0
	m.hwFirstUS = 0
	m.hwLastHost = time.Time{}
	m.hwInitialized = false
	m.pending = nil
	m.earlyTx = nil
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
	m.expireEarlyTx(now)
	for i, echo := range m.earlyTx {
		if busFrameKey(echo.frame) != busFrameKey(frame) {
			continue
		}
		m.earlyTx = append(m.earlyTx[:i], m.earlyTx[i+1:]...)
		pending := &m.pending[len(m.pending)-1]
		pending.confirmed = true
		m.relocatePending(*pending, echo.frame, echo.at, echo.wrapPeriodUS, echo.occupiedNs)
		break
	}
}

func (m *busLoadMeter) observe(frame CanFrame, now time.Time) {
	m.observeWithWrap(frame, now, 0)
}

func (m *busLoadMeter) observeWithWrap(frame CanFrame, now time.Time, wrapPeriodUS uint64) {
	m.observeWithBitCount(frame, 0, now, wrapPeriodUS)
}

// observeWithBitCount records a frame using a hardware-reported on-wire bit
// count when available. Vector's totalBitCnt covers the frame through EOF, so
// the three nominal-rate intermission bits are added to retain the meter's
// existing definition of bus occupancy.
func (m *busLoadMeter) observeWithBitCount(frame CanFrame, totalBitCount uint16, now time.Time, wrapPeriodUS uint64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.window == 0 {
		return
	}
	occupied := frameOccupancy(frame, m.nominalBitrate, m.dataBitrate)
	if totalBitCount != 0 {
		occupied = frameOccupancyFromBitCount(frame, int(totalBitCount)+3, m.nominalBitrate, m.dataBitrate)
	}
	m.expirePending(now)
	m.expireEarlyTx(now)
	if frame.Direction == TX {
		if i := m.findPending(frame, true, now); i >= 0 {
			if !m.pending[i].confirmed {
				m.pending[i].confirmed = true
				m.relocatePending(m.pending[i], frame, now, wrapPeriodUS, uint64(occupied))
			}
		} else if frame.TimestampUS != 0 {
			if len(m.earlyTx) >= maxPendingTx {
				m.earlyTx = m.earlyTx[1:]
			}
			m.earlyTx = append(m.earlyTx, earlyTxEcho{
				frame: frame, at: now, wrapPeriodUS: wrapPeriodUS, occupiedNs: uint64(occupied),
			})
		}
		return
	}
	if i := m.findPending(frame, false, now); i >= 0 {
		pending := m.pending[i]
		m.pending = append(m.pending[:i], m.pending[i+1:]...)
		if !pending.confirmed {
			m.relocatePending(pending, frame, now, wrapPeriodUS, uint64(occupied))
		}
		return
	}
	if frame.TimestampUS != 0 {
		m.addHardwareLocked(frame, now, wrapPeriodUS, uint64(occupied))
	} else {
		m.addDurationLocked(uint64(occupied), now)
	}
}

func (m *busLoadMeter) snapshot(now time.Time) BusLoadInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.window == 0 || m.nominalBitrate == 0 {
		return BusLoadInfo{}
	}
	m.rotate(now)
	if m.hwInitialized {
		m.rotateHardware(m.hardwareNow(now))
	}
	m.expirePending(now)
	m.expireEarlyTx(now)

	var occupiedNs, frames uint64
	for i := range m.slots {
		occupiedNs += m.slots[i].occupiedNs
		frames += m.slots[i].frames
		occupiedNs += m.hwSlots[i].occupiedNs
		frames += m.hwSlots[i].frames
	}

	denom := m.window
	if m.started.IsZero() {
		return BusLoadInfo{
			Window:         m.window,
			NominalBitrate: m.nominalBitrate,
			DataBitrate:    m.dataBitrate,
		}
	}
	age := now.Sub(m.started)
	if m.hwInitialized {
		hardwareAgeUS := m.hardwareNow(now) - m.hwFirstUS
		if hardwareAgeUS >= uint64(m.window/time.Microsecond) {
			age = m.window
		} else if hardwareAge := time.Duration(hardwareAgeUS) * time.Microsecond; hardwareAge > age {
			age = hardwareAge
		}
	}
	if age > 0 && age < m.window {
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
	m.addDurationLocked(uint64(occupied), now)
}

func (m *busLoadMeter) addDurationLocked(occupiedNs uint64, now time.Time) {
	occupied := time.Duration(occupiedNs)
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
	m.slots[idx].occupiedNs += occupiedNs
	m.slots[idx].frames++
}

func (m *busLoadMeter) relocatePending(pending pendingTx, frame CanFrame, now time.Time, wrapPeriodUS uint64, occupiedNs uint64) {
	if frame.TimestampUS == 0 {
		return
	}
	m.removeHostLocked(pending)
	m.addHardwareLocked(frame, now, wrapPeriodUS, occupiedNs)
}

func (m *busLoadMeter) removeHostLocked(pending pendingTx) {
	m.rotate(pending.at)
	if pending.at.Before(m.origin) {
		return
	}
	idx := int(pending.at.Sub(m.origin) / m.slot)
	if idx >= len(m.slots) {
		idx = len(m.slots) - 1
	}
	if idx < 0 || m.slots[idx].frames == 0 || m.slots[idx].occupiedNs < pending.occupiedNs {
		return
	}
	m.slots[idx].occupiedNs -= pending.occupiedNs
	m.slots[idx].frames--
}

func (m *busLoadMeter) addHardwareLocked(frame CanFrame, now time.Time, wrapPeriodUS uint64, occupiedNs uint64) {
	if occupiedNs == 0 {
		return
	}
	if m.hwInitialized {
		m.rotateHardware(m.hardwareNow(now))
	}
	atUS := m.hardwareTimestamp(frame.TimestampUS, now, wrapPeriodUS)
	m.rotateHardware(atUS)
	if atUS < m.hwOriginUS {
		for _, slot := range m.hwSlots {
			if slot.frames != 0 {
				return
			}
		}
		m.hwOriginUS = atUS
	}
	idx := int((atUS - m.hwOriginUS) / uint64(m.slot/time.Microsecond))
	if idx >= len(m.hwSlots) {
		idx = len(m.hwSlots) - 1
	}
	m.hwSlots[idx].occupiedNs += occupiedNs
	m.hwSlots[idx].frames++
	if m.started.IsZero() {
		m.started = now
	}
}

func (m *busLoadMeter) hardwareTimestamp(rawUS uint64, now time.Time, wrapPeriodUS uint64) uint64 {
	if !m.hwInitialized {
		m.hwInitialized = true
		m.hwLastRawUS = rawUS
		m.hwLastUS = rawUS
		m.hwFirstUS = rawUS
		m.hwLastHost = now
		return rawUS
	}
	if rawUS < m.hwLastRawUS {
		backward := m.hwLastRawUS - rawUS
		if wrapPeriodUS > 0 && backward > wrapPeriodUS/2 && m.hwLastRawUS < wrapPeriodUS {
			m.hwLastUS += wrapPeriodUS - m.hwLastRawUS + rawUS
		} else if backward <= uint64(m.window/time.Microsecond) && backward <= m.hwLastUS {
			return m.hwLastUS - backward
		} else {
			// The device clock restarted; old hardware buckets no longer share its epoch.
			m.hwSlots = [busLoadSlotCount]busLoadSlot{}
			m.hwOriginUS = 0
			m.hwLastUS = rawUS
			m.hwFirstUS = rawUS
		}
	} else {
		m.hwLastUS += rawUS - m.hwLastRawUS
	}
	m.hwLastRawUS = rawUS
	m.hwLastHost = now
	return m.hwLastUS
}

func (m *busLoadMeter) hardwareNow(now time.Time) uint64 {
	if elapsed := now.Sub(m.hwLastHost); elapsed > 0 {
		return m.hwLastUS + uint64(elapsed/time.Microsecond)
	}
	return m.hwLastUS
}

func (m *busLoadMeter) rotateHardware(atUS uint64) {
	if m.hwOriginUS == 0 {
		m.hwOriginUS = atUS
		return
	}
	if atUS <= m.hwOriginUS {
		return
	}
	windowUS := uint64(m.window / time.Microsecond)
	elapsed := atUS - m.hwOriginUS
	if elapsed <= windowUS {
		return
	}
	slotUS := uint64(m.slot / time.Microsecond)
	shift := (elapsed - windowUS + slotUS - 1) / slotUS
	if shift >= uint64(len(m.hwSlots)) {
		m.hwSlots = [busLoadSlotCount]busLoadSlot{}
		m.hwOriginUS = atUS
		return
	}
	copy(m.hwSlots[:], m.hwSlots[shift:])
	for i := len(m.hwSlots) - int(shift); i < len(m.hwSlots); i++ {
		m.hwSlots[i] = busLoadSlot{}
	}
	m.hwOriginUS += shift * slotUS
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
	m.pending = append(m.pending, pendingTx{
		key: busFrameKey(frame), at: now,
		occupiedNs: uint64(frameOccupancy(frame, m.nominalBitrate, m.dataBitrate)),
	})
}

func (m *busLoadMeter) findPending(frame CanFrame, unconfirmedOnly bool, now time.Time) int {
	key := busFrameKey(frame)
	for i, pending := range m.pending {
		if pending.key != key || (unconfirmedOnly && pending.confirmed) ||
			(!unconfirmedOnly && now.Sub(pending.at) > pendingTxTTL) {
			continue
		}
		return i
	}
	return -1
}

func (m *busLoadMeter) expirePending(now time.Time) {
	kept := m.pending[:0]
	for _, pending := range m.pending {
		if now.Sub(pending.at) <= pendingTxEchoTTL {
			kept = append(kept, pending)
		}
	}
	m.pending = kept
}

func (m *busLoadMeter) expireEarlyTx(now time.Time) {
	kept := m.earlyTx[:0]
	for _, echo := range m.earlyTx {
		if now.Sub(echo.at) <= pendingTxTTL {
			kept = append(kept, echo)
		}
	}
	m.earlyTx = kept
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

// frameOccupancyFromBitCount converts Vector's measured totalBitCnt into bus
// time. For BRS frames XL API does not expose separate arbitration/data phase
// counts, so the measured total is split in the same proportion as the frame
// model. Non-BRS frames use the measured count directly at the nominal rate.
func frameOccupancyFromBitCount(frame CanFrame, totalBits int, nominalBitrate, dataBitrate uint32) time.Duration {
	if totalBits <= 0 || nominalBitrate == 0 {
		return 0
	}
	if !frame.IsFD || !frame.BRS || dataBitrate == 0 {
		return bitsToDuration(totalBits, nominalBitrate)
	}

	estimatedArb, estimatedData := frameBitCounts(frame)
	estimatedTotal := estimatedArb + estimatedData
	if estimatedTotal <= 0 || estimatedData <= 0 {
		return bitsToDuration(totalBits, nominalBitrate)
	}
	arbitrationBits := (totalBits*estimatedArb + estimatedTotal/2) / estimatedTotal
	if arbitrationBits < 1 {
		arbitrationBits = 1
	}
	if arbitrationBits > totalBits {
		arbitrationBits = totalBits
	}
	dataBits := totalBits - arbitrationBits
	return bitsToDuration(arbitrationBits, nominalBitrate) + bitsToDuration(dataBits, dataBitrate)
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

	var counter canBitCounter
	counter.addCRC(0, 1) // SOF
	counter.addCRC(uint32(frame.ID), 11)
	counter.addCRC(0, 1) // RTR
	counter.addCRC(0, 1) // IDE
	counter.addCRC(0, 1) // r0
	counter.addCRC(uint32(frame.DLC&0xF), 4)
	for i := 0; i < payload; i++ {
		counter.addCRC(uint32(frame.Data[i]), 8)
	}
	counter.add(uint32(counter.crc), 15)

	return counter.total +
		1 + // CRC delimiter
		1 + // ACK
		1 + // ACK delimiter
		7 + // EOF
		3 // IFS
}

const (
	canFDCrc17FieldBits = 4 + 17 + 6 + 1 // stuff count, CRC, fixed stuff bits, delimiter
	canFDCrc21FieldBits = 4 + 21 + 7 + 1
	canFDAckTrailerBits = 1 + 1 + 7 + 3 // ACK, ACK delimiter, EOF, intermission
)

// canFDFrameBits returns the on-wire bits for an ISO CAN-FD base frame.
// Dynamic bit stuffing is counted from SOF through the data field using the
// actual identifier, DLC and payload. The ISO CRC field has a 4-bit stuff
// count and a fixed number of stuff bits, so it must not be subjected to the
// dynamic 5-bit rule.
//
// For BRS frames, the BRS bit is assigned to the nominal phase and the CRC
// delimiter to the data phase. This whole-bit model is exact when the nominal
// and data sample points are equal; otherwise the two rate-switch boundaries
// introduce only a sub-bit-time difference.
func canFDFrameBits(frame CanFrame) (arbBits, dataBits int) {
	payload := frame.DataLength()
	if payload > 64 {
		payload = 64
	}

	var counter canFDStuffCounter
	counter.add(0, 1, false) // SOF
	counter.add(uint32(frame.ID), 11, false)
	counter.add(0, 1, false) // RRS
	counter.add(0, 1, false) // IDE: base frame
	counter.add(1, 1, false) // FDF
	counter.add(0, 1, false) // res
	if frame.BRS {
		counter.add(1, 1, false) // BRS; switch after its sample point
	} else {
		counter.add(0, 1, false)
	}

	dataPhase := frame.BRS
	counter.add(0, 1, dataPhase) // ESI: locally transmitted/normal error-active frame
	counter.add(uint32(frame.DLC&0xF), 4, dataPhase)
	for i := 0; i < payload; i++ {
		counter.add(uint32(frame.Data[i]), 8, dataPhase)
	}

	crcFieldBits := canFDCrc17FieldBits
	if payload > 16 {
		crcFieldBits = canFDCrc21FieldBits
	}

	arbBits, dataBits = counter.nominal, counter.data
	if frame.BRS {
		dataBits += crcFieldBits
	} else {
		arbBits += crcFieldBits
	}
	arbBits += canFDAckTrailerBits
	return arbBits, dataBits
}

// canFDStuffCounter counts the ISO CAN-FD dynamic-stuffing region without
// allocating a temporary bit stream. The run state deliberately continues
// across the BRS boundary.
type canFDStuffCounter struct {
	nominal int
	data    int
	run     int
	prev    byte
	set     bool
}

func (c *canFDStuffCounter) add(value uint32, n int, dataPhase bool) {
	for i := n - 1; i >= 0; i-- {
		c.count(byte((value>>i)&1), dataPhase)
	}
}

func (c *canFDStuffCounter) count(bit byte, dataPhase bool) {
	bit &= 1
	c.increment(dataPhase)
	if c.set && bit == c.prev {
		c.run++
	} else {
		c.prev = bit
		c.run = 1
		c.set = true
	}
	if c.run == 5 {
		c.increment(dataPhase)
		c.prev ^= 1
		c.run = 1
	}
}

func (c *canFDStuffCounter) increment(dataPhase bool) {
	if dataPhase {
		c.data++
	} else {
		c.nominal++
	}
}

type canBitCounter struct {
	crc   uint16
	total int
	run   int
	prev  byte
	set   bool
}

func (c *canBitCounter) addCRC(value uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		bit := byte((value >> i) & 1)
		msb := byte((c.crc >> 14) & 1)
		c.crc = (c.crc << 1) & 0x7fff
		if msb^bit == 1 {
			c.crc ^= 0x4599
		}
		c.count(bit)
	}
}

func (c *canBitCounter) add(value uint32, n int) {
	for i := n - 1; i >= 0; i-- {
		c.count(byte((value >> i) & 1))
	}
}

func (c *canBitCounter) count(bit byte) {
	bit &= 1
	c.total++
	if c.set && bit == c.prev {
		c.run++
	} else {
		c.prev = bit
		c.run = 1
		c.set = true
	}
	if c.run == 5 {
		c.total++
		c.prev ^= 1
		c.run = 1
	}
}
