package main

import (
	"fmt"
	"runtime"
	"github.com/LoveWonYoung/canbuskit/driver"
)

func main() {
	fmt.Println("GOARCH =", runtime.GOARCH)
	var drv = driver.NewCanMix(driver.CANFD)
	fmt.Println("Driver initialized:", drv)
}
