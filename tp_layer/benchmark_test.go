package tp_layer

import (
	"context"
	"testing"
)

// BenchmarkTransport_Loopback simulates a loopback test to measure throughput
func BenchmarkTransport_Loopback(b *testing.B) {
	addr1 := &Address{TxID: 0x1, RxID: 0x2}
	t1 := NewTransport(addr1, DefaultConfig())

	addr2 := &Address{TxID: 0x2, RxID: 0x1}
	t2 := NewTransport(addr2, DefaultConfig())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Virtual CAN bus
	bus1to2 := make(chan CanMessage, 100)
	bus2to1 := make(chan CanMessage, 100)

	go t1.Run(ctx, bus2to1, bus1to2)
	go t2.Run(ctx, bus1to2, bus2to1)

	// Multi-frame payload (100 bytes)
	payload := make([]byte, 100)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		// Send from T1
		go t1.Send(payload)

		// Block receive on T2
		// Since Recv is non-blocking (returns nil if empty), we need to poll
		for {
			if _, ok := t2.Recv(); ok {
				break
			}
			// Spin loop (benchmark)
		}
	}
}
