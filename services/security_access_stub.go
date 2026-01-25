//go:build !windows

package services

import (
	"errors"
	"time"

	"github.com/LoveWonYoung/canbuskit/uds_client"
)

var ErrSecKeyUnsupported = errors.New("SecKey.dll is only supported on windows")

type SecurityAccess struct {
	client  *uds_client.UDSClient
	timeout time.Duration
}

func NewSecurityAccess(client *uds_client.UDSClient) *SecurityAccess {
	return &SecurityAccess{client: client}
}

func (s *SecurityAccess) SetTimeout(timeout time.Duration) {
	if s == nil {
		return
	}
	s.timeout = timeout
}

func (s *SecurityAccess) ComputeKey(_ int, _ []byte) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) ComputeKeyWithLength(_ int, _ []byte, _ int) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) SecurityAccess(_ byte, _ byte) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) SecurityAccessWithLength(_ byte, _ byte, _ int) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) SecurityAccessWithSeedData(_ byte, _ byte, _ []byte) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) SecurityAccessWithSeedDataAndLength(_ byte, _ byte, _ []byte, _ int) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) RequestSeed(_ byte, _ byte, _ []byte) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) SendKey(_ byte, _ byte, _ []byte) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) requestSeed(_ byte, _ byte, _ []byte) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}

func (s *SecurityAccess) sendKey(_ byte, _ byte, _ []byte) ([]byte, error) {
	return nil, ErrSecKeyUnsupported
}
