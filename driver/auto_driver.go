//go:build windows

package driver

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
)

// AutoDriver selects the first available CAN device driver.
// Order: TSMaster -> Toomoss.
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
	toomoss := NewToomoss(a.canType)
	if err := toomoss.Init(); err == nil {
		a.driver = toomoss
		log.Println("Auto driver selected: Toomoss")
		return nil
	} else {
		mixErr := err
		log.Printf("Auto driver: Toomoss init failed: %v", mixErr)
		tsm := NewTSMaster(a.canType)
		if err := tsm.Init(); err == nil {
			a.driver = tsm
			log.Println("Auto driver selected: TSMaster")
			return nil
		} else {
			tsmErr := err
			log.Printf("Auto driver: TSMaster init failed: %v", tsmErr)
			return fmt.Errorf("no available CAN device (tsmaster: %v; toomoss: %v)", tsmErr, mixErr)
		}
	}
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
