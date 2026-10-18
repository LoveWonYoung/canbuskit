//go:build windows

package backend

import (
	"fmt"
	"strings"
	"sync"

	"github.com/LoveWonYoung/canbuskit/v2/can"
)

type deviceFactory func(can.Config) device

// autoCandidate describes one backend considered by autoDevice.
type autoCandidate struct {
	Name string
	New  deviceFactory
}

// defaultAutoCandidates returns the default backend priority.
func defaultAutoCandidates() []autoCandidate {
	return []autoCandidate{
		{Name: "Toomoss", New: func(cfg can.Config) device { return newToomoss(cfg) }},
		{Name: "TSMaster", New: func(cfg can.Config) device { return newTSMaster(cfg, TC1016, nil) }},
		{Name: "PCAN", New: func(cfg can.Config) device { return newPCAN(cfg) }},
		{Name: "Vector", New: func(cfg can.Config) device { return newVector(cfg, vectorVN1640) }},
	}
}

// autoDevice selects the first available, mode-compatible CAN device.
type autoDevice struct {
	canType      can.Mode
	cfg          can.Config
	candidates   []autoCandidate
	mu           sync.Mutex
	driver       device
	selectedName string
}

// newAutoDevice creates an automatic device selector. If no
// candidates are supplied, defaultAutoCandidates is used.
func newAutoDevice(cfg can.Config, candidates ...autoCandidate) *autoDevice {
	if len(candidates) == 0 {
		candidates = defaultAutoCandidates()
	}
	return &autoDevice{
		canType:    cfg.Mode,
		cfg:        cfg,
		candidates: append([]autoCandidate(nil), candidates...),
	}
}

func (a *autoDevice) initialize() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.driver != nil {
		return nil
	}
	cfg, err := normalizeConfig(a.cfg)
	if err != nil {
		return err
	}
	a.cfg = cfg
	a.canType = cfg.Mode

	var errs []string
	for _, candidate := range a.candidates {
		if candidate.New == nil {
			errs = append(errs, fmt.Sprintf("%s: candidate factory is nil", strings.ToLower(candidate.Name)))
			continue
		}
		dev := candidate.New(cfg)
		if dev == nil {
			errs = append(errs, fmt.Sprintf("%s: candidate returned nil device", strings.ToLower(candidate.Name)))
			continue
		}
		if err := dev.initialize(); err != nil {
			dev.stop()
			logf(a.cfg, "Auto backend: %s initialization failed: %v", candidate.Name, err)
			errs = append(errs, fmt.Sprintf("%s: %v", strings.ToLower(candidate.Name), err))
			continue
		}
		isFD := dev.isFD()
		wantFD := cfg.Mode == can.FD
		if isFD != wantFD {
			dev.stop()
			err := fmt.Errorf("initialized in incompatible mode (want CAN-FD=%t, got CAN-FD=%t)", wantFD, isFD)
			logf(a.cfg, "Auto backend: %s rejected: %v", candidate.Name, err)
			errs = append(errs, fmt.Sprintf("%s: %v", strings.ToLower(candidate.Name), err))
			continue
		}
		a.driver = dev
		a.selectedName = candidate.Name
		logf(a.cfg, "Auto backend selected: %s", candidate.Name)
		return nil
	}

	return fmt.Errorf("no available CAN device (%s)", strings.Join(errs, "; "))
}

func (a *autoDevice) start() error {
	drv := a.getDriver()
	if drv == nil {
		return can.ErrNotOpen
	}
	return drv.start()
}

func (a *autoDevice) stop() {
	a.mu.Lock()
	drv := a.driver
	a.driver = nil
	a.selectedName = ""
	a.mu.Unlock()
	if drv != nil {
		drv.stop()
	}
}

func (a *autoDevice) write(frame can.Frame) error {
	if drv := a.getDriver(); drv != nil {
		return drv.write(frame)
	}
	return can.ErrNotOpen
}

func (a *autoDevice) subscribe(buffer int) (<-chan can.Frame, func()) {
	drv := a.getDriver()
	if drv == nil {
		return nil, func() {}
	}
	return drv.subscribe(buffer)
}

func (a *autoDevice) errors() <-chan error {
	if drv := a.getDriver(); drv != nil {
		return drv.errors()
	}
	return closedErrors
}

func (a *autoDevice) stats() can.Stats {
	if drv := a.getDriver(); drv != nil {
		return drv.stats()
	}
	return can.Stats{}
}

func (a *autoDevice) busLoad() can.BusLoad {
	if drv := a.getDriver(); drv != nil {
		return drv.busLoad()
	}
	return can.BusLoad{}
}

func (a *autoDevice) isFD() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.driver != nil {
		return a.driver.isFD()
	}
	return a.canType == can.FD
}

func (a *autoDevice) config() can.Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg
}

func (a *autoDevice) setBRS(enabled bool) {
	a.mu.Lock()
	a.cfg.BRS = enabled
	drv := a.driver
	a.mu.Unlock()
	if drv != nil {
		drv.setBRS(enabled)
	}
}

func (a *autoDevice) brs() bool {
	if drv := a.getDriver(); drv != nil {
		return drv.brs()
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cfg.BRS
}

func (a *autoDevice) selectedBackend() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.selectedName
}

func (a *autoDevice) getDriver() device {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.driver
}
