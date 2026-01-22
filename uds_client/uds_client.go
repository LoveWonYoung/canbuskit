package uds_client

import (
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/LoveWonYoung/canbuskit/driver"
	isotp "github.com/LoveWonYoung/canbuskit/tp_layer"
)

// Transport 定义了 UDS 客户端所需的 ISO-TP 传输层接口
// 这允许我们在测试中注入 Mock 对象
type Transport interface {
	Send(data []byte)
	Recv() ([]byte, bool)
	SetTxAddress(addr *isotp.Address)
	SetFDMode(isFD bool)
	Run(ctx context.Context, rxChan <-chan isotp.CanMessage, txChan chan<- isotp.CanMessage)
}

// 通道缓冲区大小常量
const (
	adapterRxBufferSize    = 100                     // 适配器接收缓冲区大小
	adapterTxBufferSize    = 100                     // 适配器发送缓冲区大小
	goroutineSleep         = 1 * time.Millisecond    // goroutine休眠时间
	recvPollInterval       = 2 * time.Millisecond    // 接收轮询间隔
	responsePendingTimeout = 5000 * time.Millisecond // Response Pending 超时
	defaultMaxRetries      = 3                       // 默认最大重试次数
)

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
	Timeout    time.Duration // 单次请求超时
	MaxRetries int           // 最大重试次数 (仅对可重试错误生效)
	RetryDelay time.Duration // 重试间隔
}

// AddressingMode 控制发送请求时使用物理/功能寻址。
type AddressingMode int

const (
	AddressPhysical AddressingMode = iota
	AddressFunctional
)

// DefaultRequestOptions 返回默认请求选项
func DefaultRequestOptions() RequestOptions {
	return RequestOptions{
		Timeout:    500 * time.Millisecond,
		MaxRetries: defaultMaxRetries,
		RetryDelay: 100 * time.Millisecond,
	}
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
	stack    Transport // 使用接口而非具体结构体
	adapter  *driver.Adapter
	cancel   context.CancelFunc // 用于控制所有后台goroutine的生命周期
	ctx      context.Context    // 客户端生命周期 context
	reqMu    sync.Mutex
	mode     AddressingMode
	funcAddr *isotp.Address
}

// NewUDSClient 是新的构造函数，负责完成所有组件的初始化和连接。
// 它接收一个CAN驱动实例和ISOTP配置。
func NewUDSClient(dev driver.CANDriver, addr *isotp.Address, cfg isotp.Config) (*UDSClient, error) {
	// 1. 初始化适配器并启动硬件驱动
	adapter, err := driver.NewAdapter(dev)
	if err != nil {
		return nil, fmt.Errorf("无法创建Toomoss适配器: %w", err)
	}

	// 2. 初始化ISOTP协议栈
	stack := isotp.NewTransport(addr, cfg)

	return newUDSClient(adapter, stack), nil
}

// newUDSClient 内部构造函数，支持依赖注入
func newUDSClient(adapter *driver.Adapter, stack Transport) *UDSClient {
	// 3. 创建用于goroutine生命周期管理的context
	ctx, cancel := context.WithCancel(context.Background())

	// 4. 创建内部通信channels，作为协议栈和适配器之间的桥梁
	rxFromAdapter := make(chan isotp.CanMessage, adapterRxBufferSize)
	txToAdapter := make(chan isotp.CanMessage, adapterTxBufferSize)

	// 5. 启动所有必要的后台goroutines ("粘合"逻辑)
	// a. 从适配器接收数据，送入协议栈
	go func() {
		for {
			select {
			case <-ctx.Done():
				return // 接收到退出信号
			default:
				if msg, ok := adapter.RxFunc(); ok {
					rxFromAdapter <- msg
				} else {
					time.Sleep(goroutineSleep) // 避免CPU空转
				}
			}
		}
	}()

	// b. 从协议栈获取待发送数据，通过适配器发送
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case msg := <-txToAdapter:
				adapter.TxFunc(msg)
			}
		}
	}()

	// c. 驱动协议栈核心状态机
	go func() {
		stack.Run(ctx, rxFromAdapter, txToAdapter)
	}()

	// d. 监听协议栈错误 logging (仅当 stack 是具体类型时，或者扩展接口支持 ErrorChan)
	// 注意：为了保持接口简洁，这里假设 Run 方法内部或外部处理错误，
	// 或者如果原来的 isotp.Transport 必须暴露 ErrorChan，我们需要在接口中添加 getter，或者在这里做类型断言。
	// 原代码直接访问 stack.ErrorChan。
	// 简单起见，如果 stack 是 *isotp.Transport，我们启动错误监听。
	if s, ok := stack.(*isotp.Transport); ok {
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case err := <-s.ErrorChan:
					log.Printf("[ISOTP Error] %v", err)
				}
			}
		}()
	}

	log.Println("UDS客户端已成功初始化并启动。")
	return &UDSClient{
		stack:   stack,
		adapter: adapter,
		cancel:  cancel,
		ctx:     ctx,
		mode:    AddressPhysical,
	}
}

// SetFunctionalAddress sets the functional address used when AddressFunctional is active.
func (c *UDSClient) SetFunctionalAddress(addr *isotp.Address) error {
	if addr == nil {
		return errors.New("functional address cannot be nil")
	}
	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	c.funcAddr = addr
	if c.mode == AddressFunctional {
		c.stack.SetTxAddress(addr)
	}
	return nil
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

// RequestWithContext 发送 UDS 请求并等待响应，支持 Context 取消。
// 这是更健壮的请求函数，支持：
//   - Context 取消
//   - 完整的 NRC 错误处理
//   - 自动重试机制 (仅对可重试错误)
//   - 响应 SID 验证
func (c *UDSClient) RequestWithContext(ctx context.Context, payload []byte, opts RequestOptions) ([]byte, error) {
	if len(payload) == 0 {
		return nil, errors.New("请求 payload 不能为空")
	}

	c.reqMu.Lock()
	defer c.reqMu.Unlock()

	if err := c.updateTxAddressLocked(c.mode); err != nil {
		return nil, err
	}

	requestSID := payload[0]
	expectedResponseSID := requestSID + 0x40 // 正响应 SID = 请求 SID + 0x40

	var lastErr error
	var lastResp []byte
	for attempt := 0; attempt <= opts.MaxRetries; attempt++ {
		if attempt > 0 {
			log.Printf("UDS 请求重试 (%d/%d), SID=0x%02X", attempt, opts.MaxRetries, requestSID)
			time.Sleep(opts.RetryDelay)
		}

		response, err := c.singleRequest(ctx, payload, opts.Timeout)
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
			return nil, err
		}

		// 验证响应 SID
		if len(response) > 0 && response[0] != expectedResponseSID {
			// 检查是否是负响应
			if response[0] == 0x7F && len(response) >= 3 {
				return response, &UDSError{
					ServiceID: response[1],
					NRC:       response[2],
					Message:   getNRCDescription(response[2]),
				}
			}
			return response, fmt.Errorf("响应 SID 不匹配: 期望 0x%02X, 收到 0x%02X", expectedResponseSID, response[0])
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
func (c *UDSClient) singleRequest(ctx context.Context, payload []byte, timeout time.Duration) ([]byte, error) {
	// 发送前清空可能存在的旧响应
	for {
		if _, ok := c.stack.Recv(); !ok {
			break
		}
	}

	c.stack.Send(payload) // 将数据包放入发送队列

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()

	// 为防止测试时未初始化 c.ctx 导致空指针，使用本地 done channel
	clientDone := (<-chan struct{})(nil)
	if c.ctx != nil {
		clientDone = c.ctx.Done()
	}

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-clientDone:
			return nil, errors.New("UDS 客户端已关闭")
		case <-deadline.C:
			return nil, fmt.Errorf("等待响应超时 (%v)", timeout)
		default:
			if data, ok := c.stack.Recv(); ok {
				// 检查是否为负响应
				if len(data) >= 3 && data[0] == 0x7F {
					nrc := data[2]
					serviceSID := data[1]

					// Response Pending - 重置超时继续等待
					if nrc == RequestCorrectlyReceived_ResponsePending {
						if !deadline.Stop() {
							select {
							case <-deadline.C:
							default:
							}
						}
						deadline.Reset(responsePendingTimeout)
						log.Printf("收到 Response Pending (SID=%02X)，继续等待...", serviceSID)
						fmt.Printf("Response Pending (SID=%02X Resp=% 02X) ，继续等待...\n", serviceSID, data)
						continue
					}

					// 其他负响应
					return data, &UDSError{
						ServiceID: serviceSID,
						NRC:       nrc,
						Message:   getNRCDescription(nrc),
					}
				}
				return data, nil
			}
			time.Sleep(recvPollInterval) // 短暂等待，避免抢占CPU
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

// SetFDMode 允许动态切换CAN FD模式。
func (c *UDSClient) SetFDMode(isFD bool) {
	c.stack.SetFDMode(isFD)
}

// Close 优雅地关闭客户端，释放所有资源。
func (c *UDSClient) Close() {
	log.Println("正在关闭UDS客户端...")
	c.cancel()        // 发送信号，停止所有后台goroutines
	c.adapter.Close() // 调用适配器的方法，关闭硬件驱动
}

// IsClosed 检查客户端是否已关闭
func (c *UDSClient) IsClosed() bool {
	select {
	case <-c.ctx.Done():
		return true
	default:
		return false
	}
}
