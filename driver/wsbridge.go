package driver

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultWSPath           = "/ws"
	defaultHandshakeWait    = 10 * time.Second
	defaultPingPeriod       = 45 * time.Second
	defaultPongWait         = 60 * time.Second
	defaultWriteWait        = 10 * time.Second
	defaultReconnectBackoff = time.Second
	maxReconnectBackoff     = 15 * time.Second
	defaultSendQueueSize    = 4096
)

type Config struct {
	ServerURL        string
	BridgeID         string
	Side             string
	DeviceID         string
	AuthToken        string
	SharedSecret     string
	Origin           string
	Channel          uint32
	ConnectTimeout   time.Duration
	HandshakeTimeout time.Duration
	SendQueueSize    int
}

type Driver struct {
	cfg     Config
	canType CanType

	ctx    context.Context
	cancel context.CancelFunc

	rxChan chan UnifiedCANMessage
	sendCh chan []byte

	mu          sync.Mutex
	conn        *websocket.Conn
	initialized bool
	started     bool
	stopped     bool

	wg sync.WaitGroup
}

type wsCANFramePayload struct {
	ID                  uint32 `json:"id"`
	CANType             string `json:"can_type"`
	Extended            bool   `json:"extended"`
	Remote              bool   `json:"remote"`
	Error               bool   `json:"error"`
	BRS                 bool   `json:"brs"`
	ESI                 bool   `json:"esi"`
	HardwareTimestampNS uint64 `json:"hardware_timestamp_ns,omitempty"`
	DLC                 uint8  `json:"dlc"`
	DataHex             string `json:"data_hex"`
}

type wsCANEnvelope struct {
	Type      string            `json:"type"`
	Version   int               `json:"version"`
	BridgeID  string            `json:"bridge_id"`
	Seq       uint64            `json:"seq"`
	TsUnixMS  int64             `json:"ts_unix_ms"`
	Direction string            `json:"direction"`
	Channel   int               `json:"channel"`
	Frame     wsCANFramePayload `json:"frame"`
}

type wsHelloMessage struct {
	Type     string `json:"type"`
	Version  int    `json:"version"`
	BridgeID string `json:"bridge_id"`
	Side     string `json:"side"`
	DeviceID string `json:"device_id"`
	AuthMode string `json:"auth_mode,omitempty"`
	TsUnixMS int64  `json:"ts_unix_ms"`
	Nonce    string `json:"nonce"`
	Auth     string `json:"auth"`
}

type wsHelloAckMessage struct {
	Type       string `json:"type"`
	BridgeID   string `json:"bridge_id"`
	AuthMode   string `json:"auth_mode"`
	PeerOnline bool   `json:"peer_online"`
}

type wsEnvelopeType struct {
	Type string `json:"type"`
}

func WSBridgeNew(canType CanType, cfg Config) *Driver {
	return &Driver{
		cfg:     cfg,
		canType: canType,
	}
}

func (d *Driver) Init() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	if d.initialized && !d.stopped {
		return nil
	}
	if err := d.cfg.normalize(); err != nil {
		return err
	}

	d.ctx, d.cancel = context.WithCancel(context.Background())
	d.rxChan = make(chan UnifiedCANMessage, RxChannelBufferSize)
	d.sendCh = make(chan []byte, d.cfg.sendQueueSize())
	d.started = false
	d.stopped = false

	conn, err := d.connect(d.ctx)
	if err != nil {
		d.cancel()
		d.ctx = nil
		d.cancel = nil
		d.rxChan = nil
		d.sendCh = nil
		return err
	}

	d.conn = conn
	d.initialized = true
	return nil
}

func (d *Driver) Start() {
	d.mu.Lock()
	if !d.initialized || d.started || d.stopped {
		d.mu.Unlock()
		return
	}
	conn := d.conn
	d.started = true
	d.mu.Unlock()

	loopErr := make(chan error, 4)
	d.wg.Add(3)
	go d.readLoop(conn, loopErr)
	go d.writeLoop(conn, loopErr)
	go d.monitor(loopErr)
}

func (d *Driver) Stop() {
	d.mu.Lock()
	if d.stopped {
		d.mu.Unlock()
		return
	}
	d.stopped = true
	cancel := d.cancel
	rxChan := d.rxChan
	d.mu.Unlock()

	if cancel != nil {
		cancel()
	}
	d.closeConn()
	d.wg.Wait()

	d.mu.Lock()
	if rxChan != nil {
		close(rxChan)
	}
	d.conn = nil
	d.cancel = nil
	d.ctx = nil
	d.rxChan = nil
	d.sendCh = nil
	d.started = false
	d.initialized = false
	d.mu.Unlock()
}

func (d *Driver) Write(id int32, data []byte) error {
	if len(data) == 0 {
		return errors.New("data length is 0")
	}
	switch d.canType {
	case CAN:
		if len(data) > 8 {
			return fmt.Errorf("data length %d exceeds CAN maximum of 8", len(data))
		}
	case CANFD:
		if len(data) > 64 {
			return fmt.Errorf("data length %d exceeds CAN-FD maximum of 64", len(data))
		}
	default:
		return errors.New("unknown CAN type")
	}

	d.mu.Lock()
	ctx := d.ctx
	sendCh := d.sendCh
	stopped := d.stopped
	d.mu.Unlock()

	if ctx == nil || sendCh == nil || stopped {
		return errors.New("wsbridge driver is not running")
	}

	payload, err := json.Marshal(d.frameToEnvelope(uint32(id), data))
	if err != nil {
		return fmt.Errorf("marshal websocket frame: %w", err)
	}

	select {
	case sendCh <- payload:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		return errors.New("websocket send buffer full")
	}
}

func (d *Driver) RxChan() <-chan UnifiedCANMessage {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.rxChan
}

func (d *Driver) Context() context.Context {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.ctx != nil {
		return d.ctx
	}
	return context.Background()
}

func (d *Driver) connect(ctx context.Context) (*websocket.Conn, error) {
	u, err := url.Parse(d.cfg.ServerURL)
	if err != nil {
		return nil, fmt.Errorf("invalid server URL: %w", err)
	}

	if u.Path == "" || u.Path == "/" {
		u.Path = defaultWSPath
	}

	query := u.Query()
	query.Set("bridge_id", d.cfg.BridgeID)
	query.Set("side", d.cfg.Side)
	u.RawQuery = query.Encode()

	dialer := websocket.Dialer{
		HandshakeTimeout: d.cfg.handshakeTimeout(),
	}
	if timeout := d.cfg.connectTimeout(); timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	headers := http.Header{}
	if origin := strings.TrimSpace(d.cfg.Origin); origin != "" {
		headers.Set("Origin", origin)
	}

	conn, _, err := dialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", u.String(), err)
	}
	conn.SetReadLimit(1 << 20)

	if err := d.performHandshake(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("handshake failed: %w", err)
	}

	return conn, nil
}

func (d *Driver) performHandshake(conn *websocket.Conn) error {
	hello := wsHelloMessage{
		Type:     "hello",
		Version:  1,
		BridgeID: d.cfg.BridgeID,
		Side:     d.cfg.Side,
		DeviceID: d.cfg.deviceID(),
	}

	switch {
	case strings.TrimSpace(d.cfg.AuthToken) != "":
		hello.AuthMode = "token"
		hello.Auth = strings.TrimSpace(d.cfg.AuthToken)
	case strings.TrimSpace(d.cfg.SharedSecret) != "":
		hello.AuthMode = "hmac_sha256"
		hello.TsUnixMS = time.Now().UnixMilli()
		hello.Nonce = randomNonce(16)
		hello.Auth = signHello(hello, d.cfg.SharedSecret)
	}

	if err := conn.WriteJSON(hello); err != nil {
		return fmt.Errorf("send hello: %w", err)
	}

	_ = conn.SetReadDeadline(time.Now().Add(d.cfg.handshakeTimeout()))
	_, payload, err := conn.ReadMessage()
	if err != nil {
		return fmt.Errorf("read hello_ack: %w", err)
	}
	_ = conn.SetReadDeadline(time.Time{})

	var envType wsEnvelopeType
	if err := json.Unmarshal(payload, &envType); err != nil {
		return fmt.Errorf("parse hello response: %w", err)
	}

	if envType.Type == "error" {
		var errMsg struct {
			Reason string `json:"reason"`
		}
		_ = json.Unmarshal(payload, &errMsg)
		return fmt.Errorf("server error: %s", errMsg.Reason)
	}
	if envType.Type != "hello_ack" {
		return fmt.Errorf("expected hello_ack, got %s", envType.Type)
	}

	var ack wsHelloAckMessage
	if err := json.Unmarshal(payload, &ack); err != nil {
		return fmt.Errorf("parse hello_ack: %w", err)
	}

	log.Printf("wsbridge connected: bridge=%s side=%s peer_online=%t auth_mode=%s",
		ack.BridgeID, d.cfg.Side, ack.PeerOnline, ack.AuthMode)

	return nil
}

func (d *Driver) readLoop(conn *websocket.Conn, loopErr chan<- error) {
	defer d.wg.Done()

	_ = conn.SetReadDeadline(time.Now().Add(defaultPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(defaultPongWait))
	})

	for {
		select {
		case <-d.Context().Done():
			return
		default:
		}

		_, payload, err := conn.ReadMessage()
		if err != nil {
			if d.isStopped() || errors.Is(err, context.Canceled) {
				return
			}
			select {
			case loopErr <- fmt.Errorf("ws read: %w", err):
			default:
			}
			return
		}

		var envType wsEnvelopeType
		if err := json.Unmarshal(payload, &envType); err != nil {
			continue
		}

		switch envType.Type {
		case "can_frame":
			var env wsCANEnvelope
			if err := json.Unmarshal(payload, &env); err != nil {
				continue
			}
			msg, err := envelopeToUnified(env)
			if err != nil {
				log.Printf("wsbridge skipped invalid frame: %v", err)
				continue
			}
			d.pushRX(msg)
		case "peer_online", "peer_offline":
			log.Printf("wsbridge peer event: %s", string(payload))
		case "error":
			var errMsg struct {
				Reason string `json:"reason"`
			}
			_ = json.Unmarshal(payload, &errMsg)
			log.Printf("wsbridge server error: %s", errMsg.Reason)
		}
	}
}

func (d *Driver) writeLoop(conn *websocket.Conn, loopErr chan<- error) {
	defer d.wg.Done()

	ticker := time.NewTicker(defaultPingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-d.Context().Done():
			return
		case data, ok := <-d.sendCh:
			if !ok {
				return
			}
			_ = conn.SetWriteDeadline(time.Now().Add(defaultWriteWait))
			if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
				if d.isStopped() {
					return
				}
				select {
				case loopErr <- fmt.Errorf("ws write: %w", err):
				default:
				}
				return
			}
		case <-ticker.C:
			_ = conn.SetWriteDeadline(time.Now().Add(defaultWriteWait))
			if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				if d.isStopped() {
					return
				}
				select {
				case loopErr <- fmt.Errorf("ws ping: %w", err):
				default:
				}
				return
			}
		}
	}
}

func (d *Driver) monitor(loopErr chan error) {
	defer d.wg.Done()

	backoff := defaultReconnectBackoff
	for {
		select {
		case <-d.Context().Done():
			return
		case err := <-loopErr:
			if err == nil || d.isStopped() {
				return
			}
			log.Printf("wsbridge reconnecting after error: %v", err)
			d.closeConn()

			for {
				if d.Context().Err() != nil || d.isStopped() {
					return
				}

				conn, dialErr := d.connect(d.Context())
				if dialErr != nil {
					log.Printf("wsbridge reconnect failed: %v", dialErr)
					time.Sleep(backoff)
					backoff *= 2
					if backoff > maxReconnectBackoff {
						backoff = maxReconnectBackoff
					}
					continue
				}

				d.setConn(conn)
				backoff = defaultReconnectBackoff
				d.wg.Add(2)
				go d.readLoop(conn, loopErr)
				go d.writeLoop(conn, loopErr)
				break
			}
		}
	}
}

func (d *Driver) pushRX(msg UnifiedCANMessage) {
	d.mu.Lock()
	rxChan := d.rxChan
	ctx := d.ctx
	d.mu.Unlock()
	if rxChan == nil || ctx == nil {
		return
	}

	select {
	case rxChan <- msg:
	case <-ctx.Done():
	default:
		log.Printf("wsbridge rx buffer full; drop frame id=0x%X", msg.ID)
	}
}

func (d *Driver) setConn(conn *websocket.Conn) {
	d.mu.Lock()
	if d.conn != nil && d.conn != conn {
		_ = d.conn.Close()
	}
	d.conn = conn
	d.mu.Unlock()
}

func (d *Driver) closeConn() {
	d.mu.Lock()
	if d.conn != nil {
		_ = d.conn.Close()
		d.conn = nil
	}
	d.mu.Unlock()
}

func (d *Driver) isStopped() bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.stopped
}

func (d *Driver) frameToEnvelope(id uint32, data []byte) wsCANEnvelope {
	direction := "left_to_right"
	if d.cfg.Side == "right" {
		direction = "right_to_left"
	}

	canType := "CAN"
	if d.canType == CANFD {
		canType = "CANFD"
	}

	return wsCANEnvelope{
		Type:      "can_frame",
		Version:   1,
		BridgeID:  d.cfg.BridgeID,
		Seq:       uint64(time.Now().UnixNano()),
		TsUnixMS:  time.Now().UnixMilli(),
		Direction: direction,
		Channel:   int(d.cfg.Channel),
		Frame: wsCANFramePayload{
			ID:      id,
			CANType: canType,
			DLC:     uint8(len(data)),
			DataHex: hex.EncodeToString(data),
		},
	}
}

func envelopeToUnified(env wsCANEnvelope) (UnifiedCANMessage, error) {
	data, err := hex.DecodeString(env.Frame.DataHex)
	if err != nil {
		return UnifiedCANMessage{}, fmt.Errorf("decode frame payload: %w", err)
	}
	if len(data) > 64 {
		return UnifiedCANMessage{}, fmt.Errorf("frame payload too large: %d", len(data))
	}

	var payload [64]byte
	copy(payload[:], data)

	return UnifiedCANMessage{
		ID:   env.Frame.ID,
		DLC:  dataLenToDLC(len(data)),
		Data: payload,
		IsFD: strings.EqualFold(env.Frame.CANType, "CANFD"),
	}, nil
}

func signHello(m wsHelloMessage, secret string) string {
	payload := strings.Join([]string{
		m.BridgeID,
		strings.ToLower(strings.TrimSpace(m.Side)),
		m.DeviceID,
		strconv.Itoa(m.Version),
		strconv.FormatInt(m.TsUnixMS, 10),
		m.Nonce,
	}, "|")

	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(payload))
	return hex.EncodeToString(mac.Sum(nil))
}

func randomNonce(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return hex.EncodeToString(buf)
}

func dataLenToDLC(length int) byte {
	if length <= 8 {
		return byte(length)
	}
	switch {
	case length <= 12:
		return 9
	case length <= 16:
		return 10
	case length <= 20:
		return 11
	case length <= 24:
		return 12
	case length <= 32:
		return 13
	case length <= 48:
		return 14
	default:
		return 15
	}
}

func (c *Config) normalize() error {
	c.ServerURL = strings.TrimSpace(c.ServerURL)
	if c.ServerURL == "" {
		return errors.New("server URL is required")
	}

	c.BridgeID = strings.TrimSpace(c.BridgeID)
	if c.BridgeID == "" {
		return errors.New("bridge ID is required")
	}

	c.Side = strings.ToLower(strings.TrimSpace(c.Side))
	if c.Side == "" {
		c.Side = "left"
	}
	if c.Side != "left" && c.Side != "right" {
		return errors.New("side must be left or right")
	}

	c.DeviceID = strings.TrimSpace(c.DeviceID)
	if c.DeviceID == "" {
		c.DeviceID = generateDeviceID(c.Side)
	}

	c.AuthToken = strings.TrimSpace(c.AuthToken)
	c.SharedSecret = strings.TrimSpace(c.SharedSecret)
	if c.AuthToken == "" && c.SharedSecret == "" {
		log.Printf("wsbridge: no websocket authentication configured")
	}

	return nil
}

func (c Config) deviceID() string {
	if strings.TrimSpace(c.DeviceID) != "" {
		return c.DeviceID
	}
	return generateDeviceID(c.Side)
}

func (c Config) connectTimeout() time.Duration {
	if c.ConnectTimeout > 0 {
		return c.ConnectTimeout
	}
	return defaultHandshakeWait
}

func (c Config) handshakeTimeout() time.Duration {
	if c.HandshakeTimeout > 0 {
		return c.HandshakeTimeout
	}
	return defaultHandshakeWait
}

func (c Config) sendQueueSize() int {
	if c.SendQueueSize > 0 {
		return c.SendQueueSize
	}
	return defaultSendQueueSize
}

func generateDeviceID(side string) string {
	buf := make([]byte, 6)
	if _, err := rand.Read(buf); err != nil {
		return side + "-" + strconv.FormatInt(time.Now().UnixNano(), 16)
	}
	return side + "-" + hex.EncodeToString(buf)
}
