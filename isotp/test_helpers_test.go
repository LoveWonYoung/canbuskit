package isotp

import "testing"

func mustNew(t testing.TB, address *Address, cfg Config) *Session {
	t.Helper()
	session, err := newSession(address, cfg, false)
	if err != nil {
		t.Fatal(err)
	}
	return session
}
