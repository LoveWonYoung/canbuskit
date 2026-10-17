package isotp

import (
	"context"
	"testing"
)

// BenchmarkTransport_Loopback simulates a loopback test to measure throughput
func BenchmarkTransport_Loopback(b *testing.B) {
	addr1 := &Address{TxID: 0x1, RxID: 0x2}
	cfg := DefaultConfig()
	cfg.STMin = 0
	t1 := mustNew(b, addr1, cfg)

	addr2 := &Address{TxID: 0x2, RxID: 0x1}
	t2 := mustNew(b, addr2, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Virtual CAN bus
	bus1to2 := make(chan linkFrame, 100)
	bus2to1 := make(chan linkFrame, 100)

	go t1.run(ctx, bus2to1, bus1to2)
	go t2.run(ctx, bus1to2, bus2to1)

	// Multi-frame payload (100 bytes)
	payload := make([]byte, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if err := t1.SendContext(ctx, payload); err != nil {
			b.Fatal(err)
		}
		<-t2.RecvChan()
	}
}
