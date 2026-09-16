package uds_client

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/LoveWonYoung/canbuskit/driver"
	isotp "github.com/LoveWonYoung/canbuskit/tp_layer"
)

// Transport 定义了 UDS 客户端所需的 ISO-TP 传输层接口
// 这允许我们在测试中注入 Mock 对象
type Transport interface {
	Send(data []byte)
	RecvChan() <-chan []byte
	SetTxAddress(addr *isotp.Address)
	SetFDMode(isFD bool)
	SetDefaultStMin(stMin int) error
	SetDefaultBlockSize(blockSize int) error
	SetManualFlowControl(enabled bool)
	Run(ctx context.Context, rxChan <-chan isotp.CanMessage, txChan chan<- isotp.CanMessage)
}

// 通道缓冲区大小常量
const (
	driverRxBufferSize     = 100                     // 驱动接收缓冲区大小
	driverTxBufferSize     = 1024                    // 驱动发送缓冲区（大块请求 + STmin=0 时 CF 突发，适当加大）
	responsePendingTimeout = 5000 * time.Millisecond // Response Pending 超时
	defaultMaxPending      = 16
	defaultMaxRetries      = 3 // 默认最大重试次数
)

var ErrClientClosed = errors.New("UDS client is closed")

// UDS 负响应码 (Negative Response Code)
const (
	PositiveResponse                                  = 0x00
	GeneralReject                                     = 0x10
	ServiceNotSupported                               = 0x11
	SubFunctionNotSupported                           = 0x12
	IncorrectMessageLengthOrInvalidFormat             = 0x13
	ResponseTooLong                                   = 0x14
	BusyRepeatRequest                                 = 0x21
	ConditionsNotCorrect                              = 0x22
	RequestSequenceError                              = 0x24
	NoResponseFromSubnetComponent                     = 0x25
	FailurePreventsExecutionOfRequestedAction         = 0x26
	RequestOutOfRange                                 = 0x31
	SecurityAccessDenied                              = 0x33
	AuthenticationRequired                            = 0x34
	InvalidKey                                        = 0x35
	ExceedNumberOfAttempts                            = 0x36
	RequiredTimeDelayNotExpired                       = 0x37
	SecureDataTransmissionRequired                    = 0x38
	SecureDataTransmissionNotAllowed                  = 0x39
	SecureDataVerificationFailed                      = 0x3A
	CertificateVerificationFailed_InvalidTimePeriod   = 0x50
	CertificateVerificationFailed_InvalidSignature    = 0x51
	CertificateVerificationFailed_InvalidChainOfTrust = 0x52
	CertificateVerificationFailed_InvalidType         = 0x53
	CertificateVerificationFailed_InvalidFormat       = 0x54
	CertificateVerificationFailed_InvalidContent      = 0x55
	CertificateVerificationFailed_InvalidScope        = 0x56
	CertificateVerificationFailed_InvalidCertificate  = 0x57
	OwnershipVerificationFailed                       = 0x58
	ChallengeCalculationFailed                        = 0x59
	SettingAccessRightsFailed                         = 0x5A
	SessionKeyCreationDerivationFailed                = 0x5B
	ConfigurationDataUsageFailed                      = 0x5C
	DeAuthenticationFailed                            = 0x5D
	UploadDownloadNotAccepted                         = 0x70
	TransferDataSuspended                             = 0x71
	GeneralProgrammingFailure                         = 0x72
	WrongBlockSequenceCounter                         = 0x73
	RequestCorrectlyReceived_ResponsePending          = 0x78
	SubFunctionNotSupportedInActiveSession            = 0x7E
	ServiceNotSupportedInActiveSession                = 0x7F
	RpmTooHigh                                        = 0x81
	RpmTooLow                                         = 0x82
	EngineIsRunning                                   = 0x83
	EngineIsNotRunning                                = 0x84
	EngineRunTimeTooLow                               = 0x85
	TemperatureTooHigh                                = 0x86
	TemperatureTooLow                                 = 0x87
	VehicleSpeedTooHigh                               = 0x88
	VehicleSpeedTooLow                                = 0x89
	ThrottlePedalTooHigh                              = 0x8A
	ThrottlePedalTooLow                               = 0x8B
	TransmissionRangeNotInNeutral                     = 0x8C
	TransmissionRangeNotInGear                        = 0x8D
	BrakeSwitchNotClosed                              = 0x8F
	ShifterLeverNotInPark                             = 0x90
	TorqueConverterClutchLocked                       = 0x91
	VoltageTooHigh                                    = 0x92
	VoltageTooLow                                     = 0x93
	ResourceTemporarilyNotAvailable                   = 0x94
	TerminationWithSignatureRequested                 = 0x3B
	AccessDenied                                      = 0x3C
	VersionNotSupported                               = 0x3D
	SecuredLinkNotSupported                           = 0x3E
	CertificateNotAvailable                           = 0x3F
	AuditTrailInformationNotAvailable                 = 0x40
)

type UDSError struct {
	ServiceID byte   // 原始服务 ID
	NRC       byte   // 负响应码
	Message   string // 错误描述
}

func (e *UDSError) Error() string {
	return fmt.Sprintf("UDS 负响应: SID=0x%02X, NRC=0x%02X (%s)", e.ServiceID, e.NRC, e.Message)
}

// IsRetryable 判断该错误是否可以重试
func (e *UDSError) IsRetryable() bool {
	switch e.NRC {
	case BusyRepeatRequest, RequestCorrectlyReceived_ResponsePending:
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
var nrcDescriptions = map[byte]string{
	PositiveResponse:                                  "PositiveResponse",
	GeneralReject:                                     "GeneralReject",
	ServiceNotSupported:                               "ServiceNotSupported",
	SubFunctionNotSupported:                           "SubFunctionNotSupported",
	IncorrectMessageLengthOrInvalidFormat:             "IncorrectMessageLengthOrInvalidFormat",
	ResponseTooLong:                                   "ResponseTooLong",
	BusyRepeatRequest:                                 "BusyRepeatRequest",
	ConditionsNotCorrect:                              "ConditionsNotCorrect",
	RequestSequenceError:                              "RequestSequenceError",
	NoResponseFromSubnetComponent:                     "NoResponseFromSubnetComponent",
	FailurePreventsExecutionOfRequestedAction:         "FailurePreventsExecutionOfRequestedAction",
	RequestOutOfRange:                                 "RequestOutOfRange",
	SecurityAccessDenied:                              "SecurityAccessDenied",
	AuthenticationRequired:                            "AuthenticationRequired",
	InvalidKey:                                        "InvalidKey",
	ExceedNumberOfAttempts:                            "ExceedNumberOfAttempts",
	RequiredTimeDelayNotExpired:                       "RequiredTimeDelayNotExpired",
	SecureDataTransmissionRequired:                    "SecureDataTransmissionRequired",
	SecureDataTransmissionNotAllowed:                  "SecureDataTransmissionNotAllowed",
	SecureDataVerificationFailed:                      "SecureDataVerificationFailed",
	CertificateVerificationFailed_InvalidTimePeriod:   "CertificateVerificationFailed_InvalidTimePeriod",
	CertificateVerificationFailed_InvalidSignature:    "CertificateVerificationFailed_InvalidSignature",
	CertificateVerificationFailed_InvalidChainOfTrust: "CertificateVerificationFailed_InvalidChainOfTrust",
	CertificateVerificationFailed_InvalidType:         "CertificateVerificationFailed_InvalidType",
	CertificateVerificationFailed_InvalidFormat:       "CertificateVerificationFailed_InvalidFormat",
	CertificateVerificationFailed_InvalidContent:      "CertificateVerificationFailed_InvalidContent",
	CertificateVerificationFailed_InvalidScope:        "CertificateVerificationFailed_InvalidScope",
	CertificateVerificationFailed_InvalidCertificate:  "CertificateVerificationFailed_InvalidCertificate",
	OwnershipVerificationFailed:                       "OwnershipVerificationFailed",
	ChallengeCalculationFailed:                        "ChallengeCalculationFailed",
	SettingAccessRightsFailed:                         "SettingAccessRightsFailed",
	SessionKeyCreationDerivationFailed:                "SessionKeyCreationDerivationFailed",
	ConfigurationDataUsageFailed:                      "ConfigurationDataUsageFailed",
	DeAuthenticationFailed:                            "DeAuthenticationFailed",
	UploadDownloadNotAccepted:                         "UploadDownloadNotAccepted",
	TransferDataSuspended:                             "TransferDataSuspended",
	GeneralProgrammingFailure:                         "GeneralProgrammingFailure",
	WrongBlockSequenceCounter:                         "WrongBlockSequenceCounter",
	RequestCorrectlyReceived_ResponsePending:          "RequestCorrectlyReceived_ResponsePending",
	SubFunctionNotSupportedInActiveSession:            "SubFunctionNotSupportedInActiveSession",
	ServiceNotSupportedInActiveSession:                "ServiceNotSupportedInActiveSession",
	RpmTooHigh:                                        "RpmTooHigh",
	RpmTooLow:                                         "RpmTooLow",
	EngineIsRunning:                                   "EngineIsRunning",
	EngineIsNotRunning:                                "EngineIsNotRunning",
	EngineRunTimeTooLow:                               "EngineRunTimeTooLow",
	TemperatureTooHigh:                                "TemperatureTooHigh",
	TemperatureTooLow:                                 "TemperatureTooLow",
	VehicleSpeedTooHigh:                               "VehicleSpeedTooHigh",
	VehicleSpeedTooLow:                                "VehicleSpeedTooLow",
	ThrottlePedalTooHigh:                              "ThrottlePedalTooHigh",
	ThrottlePedalTooLow:                               "ThrottlePedalTooLow",
	TransmissionRangeNotInNeutral:                     "TransmissionRangeNotInNeutral",
	TransmissionRangeNotInGear:                        "TransmissionRangeNotInGear",
	BrakeSwitchNotClosed:                              "BrakeSwitchNotClosed",
	ShifterLeverNotInPark:                             "ShifterLeverNotInPark",
	TorqueConverterClutchLocked:                       "TorqueConverterClutchLocked",
	VoltageTooHigh:                                    "VoltageTooHigh",
	VoltageTooLow:                                     "VoltageTooLow",
	ResourceTemporarilyNotAvailable:                   "ResourceTemporarilyNotAvailable",
	TerminationWithSignatureRequested:                 "TerminationWithSignatureRequested",
	AccessDenied:                                      "AccessDenied",
	VersionNotSupported:                               "VersionNotSupported",
	SecuredLinkNotSupported:                           "SecuredLinkNotSupported",
	CertificateNotAvailable:                           "CertificateNotAvailable",
	AuditTrailInformationNotAvailable:                 "AuditTrailInformationNotAvailable",
}

// getNRCDescription 获取 NRC 错误描述
func getNRCDescription(nrc byte) string {
	if desc, ok := nrcDescriptions[nrc]; ok {
		return desc
	}
	return "未知错误"
}

// UDSClient 是一个高级客户端，封装了所有初始化和通信的复杂性
type UDSClient struct {
	stack       Transport // 使用接口而非具体结构体
	driver      driver.CANDriver
	cancel      context.CancelFunc // 用于控制所有后台goroutine的生命周期
	ctx         context.Context    // 客户端生命周期 context
	txErrChan   chan error
	errors      chan error
	reqMu       sync.Mutex
	closeOnce   sync.Once
	wg          sync.WaitGroup
	unsubscribe func()
	mode        AddressingMode
	funcAddr    *isotp.Address
}

// NewUDSClient 是新的构造函数，负责完成所有组件的初始化和连接。
func NewUDSClient(dev driver.CANDriver, addr *isotp.Address, cfg isotp.Config) (*UDSClient, error) {
	if dev == nil {
		return nil, errors.New("CAN driver instance cannot be nil")
	}
	if err := addr.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ISO-TP address: %w", err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("invalid ISO-TP configuration: %w", err)
	}
	if err := dev.Init(); err != nil {
		return nil, fmt.Errorf("failed to initialize CAN device: %w", err)
	}
	if starter, ok := dev.(driver.ErrorStartingCANDriver); ok {
		if err := starter.StartWithError(); err != nil {
			dev.Stop()
			return nil, fmt.Errorf("failed to start CAN device: %w", err)
		}
	} else {
		dev.Start()
	}

	stack := isotp.NewTransport(addr, cfg)
	stack.SetFDMode(dev.IsFDMode())

	return newUDSClient(dev, stack), nil
}

// newUDSClient 内部构造函数，支持依赖注入
func newUDSClient(dev driver.CANDriver, stack Transport) *UDSClient {
	// 3. 创建用于goroutine生命周期管理的context
	ctx, cancel := context.WithCancel(context.Background())
	txErrChan := make(chan error, 16)
	protocolErrors := make(chan error, 16)

	// 4. 创建内部通信channels，作为协议栈和驱动之间的桥梁
	rxFromDriver := make(chan isotp.CanMessage, driverRxBufferSize)
	txToDriver := make(chan isotp.CanMessage, driverTxBufferSize)
	var driverRx <-chan driver.CanFrame
	unsubscribe := func() {}
	if subscriber, ok := dev.(driver.RxSubscriber); ok {
		driverRx, unsubscribe = subscriber.SubscribeRx(driverRxBufferSize)
	} else {
		driverRx = dev.RxChan()
	}

	client := &UDSClient{
		stack:       stack,
		driver:      dev,
		cancel:      cancel,
		ctx:         ctx,
		txErrChan:   txErrChan,
		errors:      protocolErrors,
		mode:        AddressPhysical,
		unsubscribe: unsubscribe,
	}

	// 5. 启动所有必要的后台goroutines ("粘合"逻辑)
	// a. 从驱动接收数据，转换后送入协议栈
	client.wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case raw, ok := <-driverRx:
				if !ok {
					return
				}
				msg, accepted := convertRXMessage(raw)
				if !accepted {
					continue
				}
				select {
				case <-ctx.Done():
					return
				case rxFromDriver <- msg:
				}
			}
		}
	})

	// b. 从协议栈获取待发送数据，通过驱动发送
	client.wg.Go(func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-txToDriver:
				if !ok {
					return
				}
				if err := dev.Write(int32(msg.ArbitrationID), msg.IsFD, msg.Data); err != nil {
					err = fmt.Errorf("failed to send CAN frame (id=0x%X): %w", msg.ArbitrationID, err)
					select {
					case <-ctx.Done():
						return
					case txErrChan <- err:
					default:
					}
				}
			}
		}
	})

	// c. 驱动协议栈核心状态机
	client.wg.Go(func() {
		stack.Run(ctx, rxFromDriver, txToDriver)
	})

	// d. Forward asynchronous errors exposed by the ISO-TP stack.
	if source, ok := stack.(interface{ Errors() <-chan error }); ok {
		client.wg.Go(func() {
			for {
				select {
				case <-ctx.Done():
					return
				case err, ok := <-source.Errors():
					if !ok {
						return
					}
					select {
					case protocolErrors <- err:
					default:
					}
				}
			}
		})
	}

	return client
}

// Errors reports asynchronous ISO-TP errors. The channel closes with the client.
func (c *UDSClient) Errors() <-chan error {
	if c == nil || c.errors == nil {
		return closedErrors
	}
	return c.errors
}

var closedErrors = func() <-chan error {
	ch := make(chan error)
	close(ch)
	return ch
}()

// SetFunctionalAddress sets the functional address used when AddressFunctional is active.
func (c *UDSClient) SetFunctionalAddress(addr *isotp.Address) error {
	if err := addr.Validate(); err != nil {
		return fmt.Errorf("invalid functional address: %w", err)
	}
	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	c.funcAddr = addr
	if c.mode == AddressFunctional {
		c.stack.SetTxAddress(addr)
	}
	return nil
}

// SetDefaultStMin forwards the default flow-control STmin setting to the
// underlying ISO-TP transport.
func (c *UDSClient) SetDefaultStMin(stMin int) error {
	if c == nil || c.stack == nil {
		return errors.New("UDS client transport is not initialized")
	}
	return c.stack.SetDefaultStMin(stMin)
}

// SetDefaultBlockSize forwards the default flow-control block-size setting to
// the underlying ISO-TP transport.
func (c *UDSClient) SetDefaultBlockSize(blockSize int) error {
	if c == nil || c.stack == nil {
		return errors.New("UDS client transport is not initialized")
	}
	return c.stack.SetDefaultBlockSize(blockSize)
}

// SetManualFlowControl forwards manual flow-control mode to the underlying
// ISO-TP transport.
func (c *UDSClient) SetManualFlowControl(enabled bool) {
	if c == nil || c.stack == nil {
		return
	}
	c.stack.SetManualFlowControl(enabled)
}

// SetAddressingMode switches between physical and functional addressing for requests.
func (c *UDSClient) SetAddressingMode(mode AddressingMode) error {
	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	if err := c.updateTxAddressLocked(mode); err != nil {
		return err
	}
	c.mode = mode
	return nil
}

// UseFunctionalAddress is a convenience wrapper for SetAddressingMode(AddressFunctional).
func (c *UDSClient) UseFunctionalAddress() error {
	return c.SetAddressingMode(AddressFunctional)
}

// UsePhysicalAddress is a convenience wrapper for SetAddressingMode(AddressPhysical).
func (c *UDSClient) UsePhysicalAddress() error {
	return c.SetAddressingMode(AddressPhysical)
}

func (c *UDSClient) updateTxAddressLocked(mode AddressingMode) error {
	switch mode {
	case AddressPhysical:
		c.stack.SetTxAddress(nil)
		return nil
	case AddressFunctional:
		if c.funcAddr == nil {
			return errors.New("functional address is not set")
		}
		c.stack.SetTxAddress(c.funcAddr)
		return nil
	default:
		return fmt.Errorf("unknown addressing mode: %d", mode)
	}
}

// SendAndRecv 发送一个请求并阻塞等待响应，内置超时处理。
func (c *UDSClient) SendAndRecv(payload []byte, timeout time.Duration) ([]byte, error) {
	return c.RequestWithContext(context.Background(), payload, RequestOptions{
		Timeout:    timeout,
		MaxRetries: 0, // 保持向后兼容，不重试
		RetryDelay: 0,
	})
}

// SendAndRecvWithAddressingMode sends one request with the specified addressing
// mode without changing the client's default addressing mode.
func (c *UDSClient) SendAndRecvWithAddressingMode(payload []byte, timeout time.Duration, mode AddressingMode) ([]byte, error) {
	return c.RequestWithContextAndAddressingMode(context.Background(), payload, RequestOptions{
		Timeout:    timeout,
		MaxRetries: 0, // 保持向后兼容，不重试
		RetryDelay: 0,
	}, mode)
}

// RequestWithContext 发送 UDS 请求并等待响应，支持 Context 取消。
func (c *UDSClient) RequestWithContext(ctx context.Context, payload []byte, opts RequestOptions) ([]byte, error) {
	return c.requestWithContext(ctx, payload, opts, nil)
}

// RequestWithContextAndAddressingMode sends one request with the specified
// addressing mode without changing the client's default addressing mode.
func (c *UDSClient) RequestWithContextAndAddressingMode(ctx context.Context, payload []byte, opts RequestOptions, mode AddressingMode) ([]byte, error) {
	return c.requestWithContext(ctx, payload, opts, &mode)
}

func (c *UDSClient) requestWithContext(ctx context.Context, payload []byte, opts RequestOptions, mode *AddressingMode) ([]byte, error) {
	if ctx == nil {
		return nil, errors.New("request context cannot be nil")
	}
	if len(payload) == 0 {
		return nil, errors.New("请求 payload 不能为空")
	}
	var err error
	opts, err = normalizeRequestOptions(opts)
	if err != nil {
		return nil, err
	}

	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	requestMode := c.mode
	if mode != nil {
		requestMode = *mode
	}
	if err := c.updateTxAddressLocked(requestMode); err != nil {
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
		return lastResp, fmt.Errorf("达到最大重试次数 (%d): %w", opts.MaxRetries, lastErr)
	}
	return nil, errors.New("未知错误")
}

// singleRequest 执行单次请求（不含重试逻辑）
func (c *UDSClient) singleRequest(ctx context.Context, payload []byte, opts RequestOptions, suppressPositive bool) ([]byte, error) {
	c.drainStackRecv()
	c.drainTxErrors()

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
	txErrCh := c.txErrChan

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-clientDone:
			return nil, ErrClientClosed
		case err, ok := <-txErrCh:
			if !ok {
				return nil, errors.New("CAN transmit error channel closed")
			}
			if err != nil {
				return nil, err
			}
		case <-deadline.C:
			if suppressPositive {
				return nil, nil // 抑制正响应：超时视为成功完成
			}
			return nil, fmt.Errorf("等待响应超时 (%v)", currentTimeout)
		case data, ok := <-recvCh:
			if !ok {
				return nil, errors.New("transport receive channel closed")
			}
			// 检查是否为负响应
			if len(data) >= 3 && data[0] == 0x7F {
				nrc := data[2]
				serviceSID := data[1]
				if serviceSID != requestSID {
					continue
				}

				// Response Pending - 重置超时继续等待
				if nrc == RequestCorrectlyReceived_ResponsePending {
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

type contextSender interface {
	SendContext(context.Context, []byte) error
}

func (c *UDSClient) sendPayload(ctx context.Context, payload []byte) error {
	if sender, ok := c.stack.(contextSender); ok {
		sendCtx, cancel := context.WithCancel(ctx)
		defer cancel()
		if c.ctx != nil {
			stop := context.AfterFunc(c.ctx, cancel)
			defer stop()
		}
		if err := sender.SendContext(sendCtx, payload); err != nil {
			if c.ctx != nil && c.ctx.Err() != nil {
				return ErrClientClosed
			}
			return err
		}
		return nil
	}
	if c.ctx != nil {
		select {
		case <-c.ctx.Done():
			return ErrClientClosed
		default:
		}
	}
	c.stack.Send(append([]byte(nil), payload...))
	return nil
}

func (c *UDSClient) waitForRetry(ctx context.Context, delay time.Duration) error {
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

func (c *UDSClient) drainStackRecv() {
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

func (c *UDSClient) drainTxErrors() {
	if c.txErrChan == nil {
		return
	}
	for {
		select {
		case <-c.txErrChan:
		default:
			return
		}
	}
}

// Request 简化版请求函数，使用默认选项
func (c *UDSClient) Request(payload []byte) ([]byte, error) {
	return c.RequestWithContext(context.Background(), payload, DefaultRequestOptions())
}

// RequestWithTimeout 带自定义超时的请求函数
func (c *UDSClient) RequestWithTimeout(payload []byte, timeout time.Duration) ([]byte, error) {
	opts := DefaultRequestOptions()
	opts.Timeout = timeout
	return c.RequestWithContext(context.Background(), payload, opts)
}

// Close 优雅地关闭客户端，释放所有资源。
func (c *UDSClient) Close() {
	if c == nil {
		return
	}
	c.closeOnce.Do(func() {
		if c.cancel != nil {
			c.cancel()
		}
		if c.unsubscribe != nil {
			c.unsubscribe()
		}
		if c.driver != nil {
			c.driver.Stop()
		}
		c.wg.Wait()
		if c.errors != nil {
			close(c.errors)
		}
	})
}

// IsClosed 检查客户端是否已关闭
func (c *UDSClient) IsClosed() bool {
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
