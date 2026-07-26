//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
)

type DriverFactory func(Config) CANDriver

// AutoCandidate describes one backend considered by AutoDriver.
type AutoCandidate struct {
	Name string
	New  DriverFactory
}

// DefaultAutoCandidates returns the default backend priority.
func DefaultAutoCandidates() []AutoCandidate {
	return []AutoCandidate{
		{Name: "Toomoss", New: func(cfg Config) CANDriver { return NewToomossWithConfig(cfg) }},
		{Name: "TSMaster", New: func(cfg Config) CANDriver { return NewTSMasterWithConfig(cfg, TC1016) }},
		{Name: "PCAN", New: func(cfg Config) CANDriver { return NewPCANWithConfig(cfg) }},
		{Name: "Vector", New: func(cfg Config) CANDriver { return NewVectorWithConfig(cfg, CANOEVN1640) }},
	}
}

// AutoDriver selects the first available, mode-compatible CAN device driver.
type AutoDriver struct {
	canType      CanType
	cfg          Config
	candidates   []AutoCandidate
	mu           sync.Mutex
	driver       CANDriver
	selectedName string
}

func NewAutoDriver(canType CanType) *AutoDriver {
	return NewAutoDriverWithConfig(DefaultConfig(canType, CHANNEL1))
}

// NewAutoDriverWithConfig creates an automatic driver selector. If no
// candidates are supplied, DefaultAutoCandidates is used.
func NewAutoDriverWithConfig(cfg Config, candidates ...AutoCandidate) *AutoDriver {
	if len(candidates) == 0 {
		candidates = DefaultAutoCandidates()
	}
	return &AutoDriver{
		canType:    cfg.Mode,
		cfg:        cfg,
		candidates: append([]AutoCandidate(nil), candidates...),
	}
}

func (a *AutoDriver) Init() error {
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
			errs = append(errs, fmt.Sprintf("%s: candidate returned nil driver", strings.ToLower(candidate.Name)))
			continue
		}
		if err := dev.Init(); err != nil {
			dev.Stop()
			log.Printf("Auto driver: %s init failed: %v", candidate.Name, err)
			errs = append(errs, fmt.Sprintf("%s: %v", strings.ToLower(candidate.Name), err))
			continue
		}
		isFD, ok := DetectFDMode(dev)
		wantFD := cfg.Mode == CANFD
		if !ok || isFD != wantFD {
			dev.Stop()
			err := fmt.Errorf("initialized in incompatible mode (want CAN-FD=%t, got CAN-FD=%t, capability=%t)", wantFD, isFD, ok)
			log.Printf("Auto driver: %s rejected: %v", candidate.Name, err)
			errs = append(errs, fmt.Sprintf("%s: %v", strings.ToLower(candidate.Name), err))
			continue
		}
		a.driver = dev
		a.selectedName = candidate.Name
		log.Printf("Auto driver selected: %s", candidate.Name)
		return nil
	}

	return fmt.Errorf("no available CAN device (%s)", strings.Join(errs, "; "))
}

func (a *AutoDriver) Start() {
	if drv := a.getDriver(); drv != nil {
		drv.Start()
		return
	}
	log.Println("Auto driver start called before init")
}

func (a *AutoDriver) Stop() {
	a.mu.Lock()
	drv := a.driver
	a.driver = nil
	a.selectedName = ""
	a.mu.Unlock()
	if drv != nil {
		drv.Stop()
	}
}

func (a *AutoDriver) Write(id int32, fd bool, data []byte) error {
	if drv := a.getDriver(); drv != nil {
		return drv.Write(id, fd, data)
	}
	return errors.New("driver not initialized")
}

func (a *AutoDriver) RxChan() <-chan UnifiedCANMessage {
	if drv := a.getDriver(); drv != nil {
		return drv.RxChan()
	}
	return nil
}

func (a *AutoDriver) Context() context.Context {
	if drv := a.getDriver(); drv != nil {
		return drv.Context()
	}
	return context.Background()
}

func (a *AutoDriver) IsFDMode() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if provider, ok := a.driver.(FDModeProvider); ok {
		return provider.IsFDMode()
	}
	return a.canType == CANFD
}

func (a *AutoDriver) Config() Config {
	a.mu.Lock()
	defer a.mu.Unlock()
	if provider, ok := a.driver.(ConfigProvider); ok {
		return provider.Config()
	}
	return a.cfg
}

func (a *AutoDriver) SelectedName() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.selectedName
}

func (a *AutoDriver) getDriver() CANDriver {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.driver
}
