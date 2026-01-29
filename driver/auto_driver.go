//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/LoveWonYoung/canbuskit/driver"
)

// AutoDriver selects the first available CAN device driver.
// Order: Toomoss -> TSMaster -> PCAN -> Vector.
type AutoDriver struct {
	canType CanType
	mu      sync.Mutex
	driver  CANDriver
}

func NewAutoDriver(canType CanType) *AutoDriver {
	return &AutoDriver{canType: canType}
}

func (a *AutoDriver) Init() error {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.driver != nil {
		return nil
	}
	candidates := []struct {
		name   string
		driver CANDriver
	}{
		{name: "Toomoss CAN 1 500K 2M", driver: NewToomoss(a.canType, CHANNEL1)},
		{name: "TSMaster CAN 1 500K 2M", driver: NewTSMaster(a.canType)},
		{name: "PCAN CAN 1 500K 2M", driver: NewPCAN(a.canType, CHANNEL1)},
		{name: "Vector", driver: NewVector(a.canType,driver.CANOEVN1640,driver.CHANNEL4)},
	}

	var errs []string
	for _, candidate := range candidates {
		if err := candidate.driver.Init(); err == nil {
			a.driver = candidate.driver
			log.Printf("Auto driver selected: %s", candidate.name)
			return nil
		} else {
			log.Printf("Auto driver: %s init failed: %v", candidate.name, err)
			errs = append(errs, fmt.Sprintf("%s: %v", strings.ToLower(candidate.name), err))
		}
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
	if drv := a.getDriver(); drv != nil {
		drv.Stop()
		return
	}
}

func (a *AutoDriver) Write(id int32, data []byte) error {
	if drv := a.getDriver(); drv != nil {
		return drv.Write(id, data)
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

func (a *AutoDriver) getDriver() CANDriver {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.driver
}
