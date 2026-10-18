package uds

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LoveWonYoung/canbuskit/v2/can"
	isotp "github.com/LoveWonYoung/canbuskit/v2/isotp"
)

// transport is the ISO-TP surface required by a UDS client.
type transport interface {
	SendContext(context.Context, []byte) error
	RecvChan() <-chan []byte
	SetTxAddress(addr *isotp.Address) error
	Errors() <-chan error
	Close() error
}

const (
	responsePendingTimeout = 5000 * time.Millisecond
	defaultMaxPending      = 16
	defaultMaxRetries      = 3
)

var (
	ErrClientClosed   = errors.New("UDS client is closed")
	ErrEmptyPayload   = errors.New("UDS request payload is empty")
	ErrRequestTimeout = errors.New("UDS request timed out")
)

// NRC is an ISO 14229 negative response code.
type NRC byte

// Negative response codes from ISO 14229.
const (
	PositiveResponse                                 NRC = 0x00
	GeneralReject                                        = 0x10
	ServiceNotSupported                                  = 0x11
	SubFunctionNotSupported                              = 0x12
	IncorrectMessageLengthOrInvalidFormat                = 0x13
	ResponseTooLong                                      = 0x14
	BusyRepeatRequest                                    = 0x21
	ConditionsNotCorrect                                 = 0x22
	RequestSequenceError                                 = 0x24
	NoResponseFromSubnetComponent                        = 0x25
	FailurePreventsExecutionOfRequestedAction            = 0x26
	RequestOutOfRange                                    = 0x31
	SecurityAccessDenied                                 = 0x33
	AuthenticationRequired                               = 0x34
	InvalidKey                                           = 0x35
	ExceedNumberOfAttempts                               = 0x36
	RequiredTimeDelayNotExpired                          = 0x37
	SecureDataTransmissionRequired                       = 0x38
	SecureDataTransmissionNotAllowed                     = 0x39
	SecureDataVerificationFailed                         = 0x3A
	CertificateVerificationFailedInvalidTimePeriod       = 0x50
	CertificateVerificationFailedInvalidSignature        = 0x51
	CertificateVerificationFailedInvalidChainOfTrust     = 0x52
	CertificateVerificationFailedInvalidType             = 0x53
	CertificateVerificationFailedInvalidFormat           = 0x54
	CertificateVerificationFailedInvalidContent          = 0x55
	CertificateVerificationFailedInvalidScope            = 0x56
	CertificateVerificationFailedInvalidCertificate      = 0x57
	OwnershipVerificationFailed                          = 0x58
	ChallengeCalculationFailed                           = 0x59
	SettingAccessRightsFailed                            = 0x5A
	SessionKeyCreationDerivationFailed                   = 0x5B
	ConfigurationDataUsageFailed                         = 0x5C
	DeAuthenticationFailed                               = 0x5D
	UploadDownloadNotAccepted                            = 0x70
	TransferDataSuspended                                = 0x71
	GeneralProgrammingFailure                            = 0x72
	WrongBlockSequenceCounter                            = 0x73
	RequestCorrectlyReceivedResponsePending              = 0x78
	SubFunctionNotSupportedInActiveSession               = 0x7E
	ServiceNotSupportedInActiveSession                   = 0x7F
	RpmTooHigh                                           = 0x81
	RpmTooLow                                            = 0x82
	EngineIsRunning                                      = 0x83
	EngineIsNotRunning                                   = 0x84
	EngineRunTimeTooLow                                  = 0x85
	TemperatureTooHigh                                   = 0x86
	TemperatureTooLow                                    = 0x87
	VehicleSpeedTooHigh                                  = 0x88
	VehicleSpeedTooLow                                   = 0x89
	ThrottlePedalTooHigh                                 = 0x8A
	ThrottlePedalTooLow                                  = 0x8B
	TransmissionRangeNotInNeutral                        = 0x8C
	TransmissionRangeNotInGear                           = 0x8D
	BrakeSwitchNotClosed                                 = 0x8F
	ShifterLeverNotInPark                                = 0x90
	TorqueConverterClutchLocked                          = 0x91
	VoltageTooHigh                                       = 0x92
	VoltageTooLow                                        = 0x93
	ResourceTemporarilyNotAvailable                      = 0x94
	TerminationWithSignatureRequested                    = 0x3B
	AccessDenied                                         = 0x3C
	VersionNotSupported                                  = 0x3D
	SecuredLinkNotSupported                              = 0x3E
	CertificateNotAvailable                              = 0x3F
	AuditTrailInformationNotAvailable                    = 0x40
)

type UDSError struct {
	ServiceID byte   // 原始服务 ID
	NRC       NRC    // 负响应码
	Message   string // 错误描述
}

func (e *UDSError) Error() string {
	return fmt.Sprintf("UDS negative response: SID=0x%02X, NRC=0x%02X (%s)", e.ServiceID, e.NRC, e.Message)
}

// IsRetryable 判断该错误是否可以重试
func (e *UDSError) IsRetryable() bool {
	switch e.NRC {
	case BusyRepeatRequest, RequestCorrectlyReceivedResponsePending:
		return true
	default:
		return false
	}
}

// RequestOptions 请求配置选项
type RequestOptions struct {
	Timeout                time.Duration // 单次请求超时
	MaxRetries             int           // 最大重试次数 (仅对可重试错误生效)
	RetryDelay             time.Duration // 重试间隔
	ResponsePendingTimeout time.Duration // 每个 0x78 后等待最终响应的时间
	MaxResponsePending     int           // 单次请求允许的最大 0x78 数量
}

// AddressingMode 控制发送请求时使用物理/功能寻址。
type AddressingMode int

const (
	AddressPhysical AddressingMode = iota
	AddressFunctional
)

// hasSubFunctionSuppressPositive 判断该服务是否有首字节子功能，且允许使用 bit7 抑制正响应
func hasSubFunctionSuppressPositive(sid byte) bool {
	switch sid {
	case 0x10, 0x11, 0x19, 0x27, 0x28, 0x2F, 0x31, 0x3E, 0x85, 0x86, 0x87:
		return true
	default:
		return false
	}
}

// DefaultRequestOptions 返回默认请求选项
func DefaultRequestOptions() RequestOptions {
	return RequestOptions{
		Timeout:                500 * time.Millisecond,
		MaxRetries:             defaultMaxRetries,
		RetryDelay:             100 * time.Millisecond,
		ResponsePendingTimeout: responsePendingTimeout,
		MaxResponsePending:     defaultMaxPending,
	}
}

func normalizeRequestOptions(opts RequestOptions) (RequestOptions, error) {
	if opts.Timeout <= 0 {
		return RequestOptions{}, errors.New("request timeout must be greater than zero")
	}
	if opts.MaxRetries < 0 {
		return RequestOptions{}, errors.New("maximum retries must be >= 0")
	}
	if opts.RetryDelay < 0 {
		return RequestOptions{}, errors.New("retry delay must be >= 0")
	}
	if opts.ResponsePendingTimeout == 0 {
		opts.ResponsePendingTimeout = responsePendingTimeout
	}
	if opts.ResponsePendingTimeout < 0 {
		return RequestOptions{}, errors.New("response-pending timeout must be >= 0")
	}
	if opts.MaxResponsePending == 0 {
		opts.MaxResponsePending = defaultMaxPending
	}
	if opts.MaxResponsePending < 0 {
		return RequestOptions{}, errors.New("maximum response-pending count must be >= 0")
	}
	return opts, nil
}

// nrcDescriptions 缓存 NRC 错误描述，避免重复创建 map
var nrcDescriptions = map[NRC]string{
	PositiveResponse:                                 "PositiveResponse",
	GeneralReject:                                    "GeneralReject",
	ServiceNotSupported:                              "ServiceNotSupported",
	SubFunctionNotSupported:                          "SubFunctionNotSupported",
	IncorrectMessageLengthOrInvalidFormat:            "IncorrectMessageLengthOrInvalidFormat",
	ResponseTooLong:                                  "ResponseTooLong",
	BusyRepeatRequest:                                "BusyRepeatRequest",
	ConditionsNotCorrect:                             "ConditionsNotCorrect",
	RequestSequenceError:                             "RequestSequenceError",
	NoResponseFromSubnetComponent:                    "NoResponseFromSubnetComponent",
	FailurePreventsExecutionOfRequestedAction:        "FailurePreventsExecutionOfRequestedAction",
	RequestOutOfRange:                                "RequestOutOfRange",
	SecurityAccessDenied:                             "SecurityAccessDenied",
	AuthenticationRequired:                           "AuthenticationRequired",
	InvalidKey:                                       "InvalidKey",
	ExceedNumberOfAttempts:                           "ExceedNumberOfAttempts",
	RequiredTimeDelayNotExpired:                      "RequiredTimeDelayNotExpired",
	SecureDataTransmissionRequired:                   "SecureDataTransmissionRequired",
	SecureDataTransmissionNotAllowed:                 "SecureDataTransmissionNotAllowed",
	SecureDataVerificationFailed:                     "SecureDataVerificationFailed",
	CertificateVerificationFailedInvalidTimePeriod:   "CertificateVerificationFailedInvalidTimePeriod",
	CertificateVerificationFailedInvalidSignature:    "CertificateVerificationFailedInvalidSignature",
	CertificateVerificationFailedInvalidChainOfTrust: "CertificateVerificationFailedInvalidChainOfTrust",
	CertificateVerificationFailedInvalidType:         "CertificateVerificationFailedInvalidType",
	CertificateVerificationFailedInvalidFormat:       "CertificateVerificationFailedInvalidFormat",
	CertificateVerificationFailedInvalidContent:      "CertificateVerificationFailedInvalidContent",
	CertificateVerificationFailedInvalidScope:        "CertificateVerificationFailedInvalidScope",
	CertificateVerificationFailedInvalidCertificate:  "CertificateVerificationFailedInvalidCertificate",
	OwnershipVerificationFailed:                      "OwnershipVerificationFailed",
	ChallengeCalculationFailed:                       "ChallengeCalculationFailed",
	SettingAccessRightsFailed:                        "SettingAccessRightsFailed",
	SessionKeyCreationDerivationFailed:               "SessionKeyCreationDerivationFailed",
	ConfigurationDataUsageFailed:                     "ConfigurationDataUsageFailed",
	DeAuthenticationFailed:                           "DeAuthenticationFailed",
	UploadDownloadNotAccepted:                        "UploadDownloadNotAccepted",
	TransferDataSuspended:                            "TransferDataSuspended",
	GeneralProgrammingFailure:                        "GeneralProgrammingFailure",
	WrongBlockSequenceCounter:                        "WrongBlockSequenceCounter",
	RequestCorrectlyReceivedResponsePending:          "RequestCorrectlyReceivedResponsePending",
	SubFunctionNotSupportedInActiveSession:           "SubFunctionNotSupportedInActiveSession",
	ServiceNotSupportedInActiveSession:               "ServiceNotSupportedInActiveSession",
	RpmTooHigh:                                       "RpmTooHigh",
	RpmTooLow:                                        "RpmTooLow",
	EngineIsRunning:                                  "EngineIsRunning",
	EngineIsNotRunning:                               "EngineIsNotRunning",
	EngineRunTimeTooLow:                              "EngineRunTimeTooLow",
	TemperatureTooHigh:                               "TemperatureTooHigh",
	TemperatureTooLow:                                "TemperatureTooLow",
	VehicleSpeedTooHigh:                              "VehicleSpeedTooHigh",
	VehicleSpeedTooLow:                               "VehicleSpeedTooLow",
	ThrottlePedalTooHigh:                             "ThrottlePedalTooHigh",
	ThrottlePedalTooLow:                              "ThrottlePedalTooLow",
	TransmissionRangeNotInNeutral:                    "TransmissionRangeNotInNeutral",
	TransmissionRangeNotInGear:                       "TransmissionRangeNotInGear",
	BrakeSwitchNotClosed:                             "BrakeSwitchNotClosed",
	ShifterLeverNotInPark:                            "ShifterLeverNotInPark",
	TorqueConverterClutchLocked:                      "TorqueConverterClutchLocked",
	VoltageTooHigh:                                   "VoltageTooHigh",
	VoltageTooLow:                                    "VoltageTooLow",
	ResourceTemporarilyNotAvailable:                  "ResourceTemporarilyNotAvailable",
	TerminationWithSignatureRequested:                "TerminationWithSignatureRequested",
	AccessDenied:                                     "AccessDenied",
	VersionNotSupported:                              "VersionNotSupported",
	SecuredLinkNotSupported:                          "SecuredLinkNotSupported",
	CertificateNotAvailable:                          "CertificateNotAvailable",
	AuditTrailInformationNotAvailable:                "AuditTrailInformationNotAvailable",
}

// getNRCDescription 获取 NRC 错误描述
func getNRCDescription(nrc NRC) string {
	if desc, ok := nrcDescriptions[nrc]; ok {
		return desc
	}
	return "UnknownNRC"
}

// UDSClient 是一个高级客户端，封装了所有初始化和通信的复杂性
type Client struct {
	stack     transport
	cancel    context.CancelFunc
	ctx       context.Context
	reqMu     sync.Mutex
	closeOnce sync.Once
	closeErr  error
	funcAddr  *isotp.Address
}

// New creates a protocol client over an already-open CAN driver. The caller
// retains ownership of dev and must close it after closing the client.
func New(dev can.Driver, addr *isotp.Address, cfg isotp.Config) (*Client, error) {
	if dev == nil {
		return nil, errors.New("CAN driver instance cannot be nil")
	}
	if err := addr.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ISO-TP address: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ISO-TP configuration: %w", err)
	}
	stack, err := isotp.New(dev, addr, cfg)
	if err != nil {
		return nil, fmt.Errorf("create ISO-TP session: %w", err)
	}

	return newClient(stack), nil
}

// newClient 内部构造函数，支持依赖注入
func newClient(stack transport) *Client {
	ctx, cancel := context.WithCancel(context.Background())

	client := &Client{
		stack:  stack,
		cancel: cancel,
		ctx:    ctx,
	}

	return client
}

// Errors reports asynchronous ISO-TP errors. The channel closes with the client.
func (c *Client) Errors() <-chan error {
	if c == nil || c.stack == nil {
		return closedErrors
	}
	return c.stack.Errors()
}

var closedErrors = func() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}()

// SetFunctionalAddress sets the address used by functional requests.
func (c *Client) SetFunctionalAddress(addr *isotp.Address) error {
	if err := addr.Validate(); err != nil {
		return fmt.Errorf("invalid functional address: %w", err)
	}
	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	c.funcAddr = &isotp.Address{TxID: addr.TxID, RxID: addr.RxID}
	return nil
}

func (c *Client) setTxAddressLocked(mode AddressingMode) error {
	switch mode {
	case AddressPhysical:
		return c.stack.SetTxAddress(nil)
	case AddressFunctional:
		if c.funcAddr == nil {
			return errors.New("functional address is not set")
		}
		return c.stack.SetTxAddress(c.funcAddr)
	default:
		return fmt.Errorf("unknown addressing mode: %d", mode)
	}
}

// RequestWithContext 发送 UDS 请求并等待响应，支持 Context 取消。
func (c *Client) RequestWithContext(ctx context.Context, payload []byte, opts RequestOptions) ([]byte, error) {
	return c.requestWithContext(ctx, payload, opts, nil)
}

// RequestWithContextAndAddressingMode sends one request with the specified
// addressing mode. Regular requests always use physical addressing.
func (c *Client) RequestWithContextAndAddressingMode(ctx context.Context, payload []byte, opts RequestOptions, mode AddressingMode) ([]byte, error) {
	return c.requestWithContext(ctx, payload, opts, &mode)
}

func (c *Client) requestWithContext(ctx context.Context, payload []byte, opts RequestOptions, mode *AddressingMode) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("request context cannot be nil")
	}
	if len(payload) == 0 {
		return nil, ErrEmptyPayload
	}
	var err error
	opts, err = normalizeRequestOptions(opts)
	if err != nil {
		return nil, err
	}

	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	requestMode := AddressPhysical
	if mode != nil {
		requestMode = *mode
	}
	if err := c.setTxAddressLocked(requestMode); err != nil {
		return nil, err
	}

	requestSID := payload[0]
	suppressPositive := hasSubFunctionSuppressPositive(requestSID) && len(payload) >= 2 && (payload[1]&0x80) != 0 // 仅对子功能服务识别 bit7

	var lastErr error
	var lastResp []byte
	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		if attempt > 0 {
			if err := c.waitForRetry(ctx, opts.RetryDelay); err != nil {
				return nil, err
			}
		}

		response, err := c.singleRequest(ctx, payload, opts, suppressPositive)
		if err != nil {
			// 检查是否是 context 取消
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, err
			}

			// 检查是否是 UDS 错误
			var udsErr *UDSError
			if errors.As(err, &udsErr) {
				// 可重试的 UDS 错误 -> 记录最后一次错误和响应，然后重试
				if udsErr.IsRetryable() && attempt < opts.MaxRetries {
					lastErr = err
					lastResp = response
					continue
				}
				// 不可重试的 UDS 错误 -> 返回原始响应和错误
				return response, err
			}

			// 其他错误
			return response, err
		}

		return response, nil
	}

	if lastErr != nil {
		// 如果有最后一次响应，返回它以便调用方能查看原始帧
		return lastResp, fmt.Errorf("maximum retry count reached (%d): %w", opts.MaxRetries, lastErr)
	}
	return nil, errors.New("request failed without a response")
}

// singleRequest 执行单次请求（不含重试逻辑）
func (c *Client) singleRequest(ctx context.Context, payload []byte, opts RequestOptions, suppressPositive bool) ([]byte, error) {
	c.drainStackRecv()

	if err := c.sendPayload(ctx, payload); err != nil {
		return nil, err
	}

	deadline := time.NewTimer(opts.Timeout)
	defer deadline.Stop()
	currentTimeout := opts.Timeout
	pendingCount := 0
	requestSID := payload[0]
	expectedResponseSID := requestSID + 0x40

	// 为防止测试时未初始化 c.ctx 导致空指针，使用本地 done channel
	clientDone := (<-chan struct{})(nil)
	if c.ctx != nil {
		clientDone = c.ctx.Done()
	}

	recvCh := c.stack.RecvChan()

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-clientDone:
			return nil, ErrClientClosed
		case <-deadline.C:
			if suppressPositive {
				return nil, nil // 抑制正响应：超时视为成功完成
			}
			return nil, fmt.Errorf("%w after %v", ErrRequestTimeout, currentTimeout)
		case data, ok := <-recvCh:
			if !ok {
				return nil, errors.New("transport receive channel closed")
			}
			// 检查是否为负响应
			if len(data) >= 3 && data[0] == 0x7F {
				nrc := NRC(data[2])
				serviceSID := data[1]
				if serviceSID != requestSID {
					continue
				}

				// Response Pending - 重置超时继续等待
				if nrc == RequestCorrectlyReceivedResponsePending {
					pendingCount++
					if pendingCount > opts.MaxResponsePending {
						return data, fmt.Errorf("too many UDS response-pending replies: %d", pendingCount)
					}
					resetTimer(deadline, opts.ResponsePendingTimeout)
					currentTimeout = opts.ResponsePendingTimeout
					continue
				}

				// 其他负响应
				return data, &UDSError{
					ServiceID: serviceSID,
					NRC:       nrc,
					Message:   getNRCDescription(nrc),
				}
			}
			if len(data) == 0 || data[0] != expectedResponseSID {
				continue
			}
			return data, nil
		}
	}
}

func (c *Client) sendPayload(ctx context.Context, payload []byte) error {
	sendCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if c.ctx != nil {
		stop := context.AfterFunc(c.ctx, cancel)
		defer stop()
	}
	if err := c.stack.SendContext(sendCtx, payload); err != nil {
		if c.ctx != nil && c.ctx.Err() != nil {
			return ErrClientClosed
		}
		return err
	}
	return nil
}

func (c *Client) waitForRetry(ctx context.Context, delay time.Duration) error {
	if delay == 0 {
		return nil
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	clientDone := (<-chan struct{})(nil)
	if c.ctx != nil {
		clientDone = c.ctx.Done()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-clientDone:
		return ErrClientClosed
	case <-timer.C:
		return nil
	}
}

func resetTimer(timer *time.Timer, timeout time.Duration) {
	if !timer.Stop() {
		select {
		case <-timer.C:
		default:
		}
	}
	timer.Reset(timeout)
}

func (c *Client) drainStackRecv() {
	for {
		select {
		case _, ok := <-c.stack.RecvChan():
			if !ok {
				return
			}
		default:
			return
		}
	}
}

// Request 简化版请求函数，使用默认选项
func (c *Client) Request(payload []byte) ([]byte, error) {
	return c.RequestWithContext(context.Background(), payload, DefaultRequestOptions())
}

// RequestWithTimeout 带自定义超时的请求函数
func (c *Client) RequestWithTimeout(payload []byte, timeout time.Duration) ([]byte, error) {
	opts := DefaultRequestOptions()
	opts.Timeout = timeout
	return c.RequestWithContext(context.Background(), payload, opts)
}

// Close 优雅地关闭客户端，释放所有资源。
func (c *Client) Close() error {
	if c == nil {
		return nil
	}
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		if c.stack != nil {
			c.closeErr = c.stack.Close()
		}
	})
	return c.closeErr
}

// IsClosed 检查客户端是否已关闭
func (c *Client) IsClosed() bool {
	if c == nil || c.ctx == nil {
		return false
	}
	select {
	case <-c.ctx.Done():
		return true
	default:
		return false
	}
}
