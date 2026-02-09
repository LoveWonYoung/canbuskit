//go:build windows

package services

import (
	"errors"
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/LoveWonYoung/canbuskit/uds_client"
)

const defaultSecKeyDLL = "SecKey.dll"

const (
	securityAccessModeRequestSeed = iota
	securityAccessModeSendKey
)

type SecurityAccess struct {
	client  *uds_client.UDSClient
	dllPath string
	once    sync.Once
	dll     *syscall.LazyDLL
	proc    *syscall.LazyProc
	loadErr error
	timeout time.Duration
}

func NewSecurityAccess(client *uds_client.UDSClient) *SecurityAccess {
	return &SecurityAccess{
		client:  client,
		dllPath: defaultSecKeyDLL,
	}
}

func (s *SecurityAccess) SetTimeout(timeout time.Duration) {
	if s == nil {
		return
	}
	s.timeout = timeout
}

func (s *SecurityAccess) SecurityAccess(serviceID, level byte) ([]byte, error) {
	return s.SecurityAccessWithSeedDataAndLength(serviceID, level, nil, 0)
}

func (s *SecurityAccess) SecurityAccessWithLength(serviceID, level byte, keyLen int) ([]byte, error) {
	return s.SecurityAccessWithSeedDataAndLength(serviceID, level, nil, keyLen)
}

func (s *SecurityAccess) SecurityAccessWithSeedData(serviceID, level byte, seedData []byte) ([]byte, error) {
	return s.SecurityAccessWithSeedDataAndLength(serviceID, level, seedData, 0)
}

func (s *SecurityAccess) SecurityAccessWithSeedDataAndLength(serviceID, level byte, seedData []byte, keyLen int) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("uds client is nil")
	}

	seedLevel, err := normalizeSecurityLevel(securityAccessModeRequestSeed, level)
	if err != nil {
		return nil, err
	}
	keyLevel, err := normalizeSecurityLevel(securityAccessModeSendKey, level)
	if err != nil {
		return nil, err
	}

	seed, err := s.requestSeed(serviceID, seedLevel, seedData)
	if err != nil {
		return nil, err
	}

	key, err := s.ComputeKeyWithLength(int(seedLevel), seed, keyLen)
	if err != nil {
		return nil, err
	}

	return s.sendKey(serviceID, keyLevel, key)
}

func (s *SecurityAccess) ComputeKey(level int, seed []byte) ([]byte, error) {
	return s.ComputeKeyWithLength(level, seed, 0)
}

func (s *SecurityAccess) ComputeKeyWithLength(level int, seed []byte, keyLen int) ([]byte, error) {
	if err := s.load(); err != nil {
		return nil, err
	}
	if len(seed) == 0 {
		return nil, errors.New("seed is empty")
	}

	if keyLen < 0 {
		return nil, errors.New("key length must be >= 0")
	}
	if keyLen == 0 {
		keyLen = len(seed)
	}
	key := make([]byte, keyLen)
	_, _, callErr := s.proc.Call(
		uintptr(level),
		uintptr(unsafe.Pointer(&seed[0])),
		uintptr(unsafe.Pointer(&key[0])),
	)
	runtime.KeepAlive(seed)
	if !errors.Is(callErr, syscall.Errno(0)) {
		return nil, fmt.Errorf("SecKeyCmac call failed: %w", callErr)
	}
	return key, nil
}

func (s *SecurityAccess) RequestSeed(serviceID, level byte, data []byte) ([]byte, error) {
	seedLevel, err := normalizeSecurityLevel(securityAccessModeRequestSeed, level)
	if err != nil {
		return nil, err
	}
	return s.requestSeed(serviceID, seedLevel, data)
}

func (s *SecurityAccess) SendKey(serviceID, level byte, key []byte) ([]byte, error) {
	keyLevel, err := normalizeSecurityLevel(securityAccessModeSendKey, level)
	if err != nil {
		return nil, err
	}
	return s.sendKey(serviceID, keyLevel, key)
}

func (s *SecurityAccess) requestSeed(serviceID, level byte, data []byte) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("uds client is nil")
	}

	req := make([]byte, 0, 2+len(data))
	req = append(req, serviceID, level)
	if len(data) > 0 {
		req = append(req, data...)
	}

	seedResp, err := s.client.SendAndRecv(req, resolveTimeout(s.timeout))
	if err != nil {
		return nil, err
	}
	if err := validatePositiveResponse(seedResp, serviceID, level); err != nil {
		return nil, err
	}
	seed := seedResp[2:]
	if len(seed) == 0 {
		return nil, errors.New("seed is empty")
	}
	return seed, nil
}

func (s *SecurityAccess) sendKey(serviceID, level byte, key []byte) ([]byte, error) {
	if s == nil || s.client == nil {
		return nil, errors.New("uds client is nil")
	}

	req := make([]byte, 0, 2+len(key))
	req = append(req, serviceID, level)
	if len(key) > 0 {
		req = append(req, key...)
	}

	keyResp, err := s.client.SendAndRecv(req, resolveTimeout(s.timeout))
	if err != nil {
		return nil, err
	}
	if err := validatePositiveResponse(keyResp, serviceID, level); err != nil {
		return nil, err
	}
	return keyResp, nil
}

func normalizeSecurityLevel(mode int, level byte) (byte, error) {
	if level < 1 || level > 0x7E {
		return 0, fmt.Errorf("security level out of range: 0x%02X", level)
	}

	switch mode {
	case securityAccessModeRequestSeed:
		if level%2 == 0 {
			level--
		}
		return level, nil
	case securityAccessModeSendKey:
		if level%2 == 1 {
			level++
		}
		return level, nil
	default:
		return 0, fmt.Errorf("unsupported security access mode: %d", mode)
	}
}

func (s *SecurityAccess) load() error {
	s.once.Do(func() {
		s.dll = syscall.NewLazyDLL(s.dllPath)
		if err := s.dll.Load(); err != nil {
			s.loadErr = fmt.Errorf("load %s: %w", s.dllPath, err)
			return
		}

		s.proc = s.dll.NewProc("SecKeyCmac")
		if err := s.proc.Find(); err != nil {
			s.loadErr = fmt.Errorf("find SecKeyCmac in %s: %w", s.dllPath, err)
		}
	})
	return s.loadErr
}
