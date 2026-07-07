package driver

import "sync/atomic"

var printLog atomic.Bool

func SetPrintLog(b bool) {
	printLog.Store(b)
}

func printLogEnabled() bool {
	return printLog.Load()
}
