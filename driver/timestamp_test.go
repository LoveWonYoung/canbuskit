package driver

import "testing"

func TestRelativeLogClock(t *testing.T) {
	var clock relativeLogClock

	if elapsed, delta := clock.observe(1_000_000, 0); elapsed != 0 || delta != 0 {
		t.Fatalf("first frame = (%d, %d), want (0, 0)", elapsed, delta)
	}
	if elapsed, delta := clock.observe(1_001_250, 0); elapsed != 1_250 || delta != 1_250 {
		t.Fatalf("second frame = (%d, %d), want (1250, 1250)", elapsed, delta)
	}
	if elapsed, delta := clock.observe(1_004_000, 0); elapsed != 4_000 || delta != 2_750 {
		t.Fatalf("third frame = (%d, %d), want (4000, 2750)", elapsed, delta)
	}

	clock.reset()
	if elapsed, delta := clock.observe(42, 0); elapsed != 0 || delta != 0 {
		t.Fatalf("first frame after reset = (%d, %d), want (0, 0)", elapsed, delta)
	}
}

func TestRelativeLogClockUnwrapsKnownCounter(t *testing.T) {
	var clock relativeLogClock
	const wrap = uint64(1_000)

	clock.observe(990, wrap)
	if elapsed, delta := clock.observe(5, wrap); elapsed != 15 || delta != 15 {
		t.Fatalf("wrapped frame = (%d, %d), want (15, 15)", elapsed, delta)
	}
	if elapsed, delta := clock.observe(25, wrap); elapsed != 35 || delta != 20 {
		t.Fatalf("post-wrap frame = (%d, %d), want (35, 20)", elapsed, delta)
	}
}

func TestRelativeLogClockDoesNotUnderflowOnUnknownRollback(t *testing.T) {
	var clock relativeLogClock
	clock.observe(500, 0)

	if elapsed, delta := clock.observe(20, 0); elapsed != 0 || delta != 0 {
		t.Fatalf("rollback frame = (%d, %d), want (0, 0)", elapsed, delta)
	}
	if elapsed, delta := clock.observe(35, 0); elapsed != 15 || delta != 15 {
		t.Fatalf("post-rollback frame = (%d, %d), want (15, 15)", elapsed, delta)
	}
}
