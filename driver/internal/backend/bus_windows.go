//go:build windows

package backend

import "github.com/LoveWonYoung/canbuskit/v2/can"

func Auto(cfg can.Config) can.Driver {
	return newBus(newAutoDevice(cfg))
}

func ToomossBus(cfg can.Config) can.Driver {
	return newBus(newToomoss(cfg))
}

func PCANBus(cfg can.Config) can.Driver {
	return newBus(newPCAN(cfg))
}

func VectorBus(cfg can.Config, deviceType int) can.Driver {
	return newBus(newVector(cfg, deviceType))
}

func CanalystIIBus(cfg can.Config, deviceType, deviceIndex int) can.Driver {
	return newBus(newCanalystII(cfg, deviceType, deviceIndex))
}

func TSMasterBus(cfg can.Config, deviceType int, mapping *TSMasterMapping) can.Driver {
	return newBus(newTSMaster(cfg, deviceType, mapping))
}
