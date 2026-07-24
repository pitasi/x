package jetstream

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"
	"math"
	"math/rand/v2"
	"net/url"
	"strconv"
	"time"

	"anto.pt/x/atproto-exporter/internal/cursor"
	"anto.pt/x/atproto-exporter/internal/metrics"

	"github.com/coder/websocket"
	"github.com/klauspost/compress/zstd"
)

// zstdDict is the Jetstream /subscribe compression dictionary. Refresh it with
// `make update-zstd-dict` if Bluesky rotates it.
//
//go:embed zstd_dict.bin
var zstdDict []byte

const readLimit = 16 << 20 // 16 MiB, generous for a single firehose message

// wsConn is the minimal WebSocket surface the consumer needs; abstracted so
// tests can inject a fake connection with no live network.
type wsConn interface {
	Read(ctx context.Context) ([]byte, error)
	Close() error
}

// DialFunc opens a connection to a Jetstream subscribe URL.
type DialFunc func(ctx context.Context, url string) (wsConn, error)

// Config configures a Consumer.
type Config struct {
	Hosts       []string
	Collections []string
	Zstd        bool
	Rewind      time.Duration

	BackoffBase time.Duration
	BackoffMax  time.Duration

	// Optional injection points (defaults are used when nil).
	Dial   DialFunc
	Sleep  func(ctx context.Context, d time.Duration) error
	Jitter func() float64 // returns [0,1)
}

// Consumer maintains a single Jetstream connection, feeding decoded (and
// optionally zstd-decompressed) messages into the pipeline. It reconnects with
// exponential backoff + jitter and rotates across the configured hosts.
type Consumer struct {
	cfg      Config
	pipeline *Pipeline
	m        *metrics.Metrics
	cur      *cursor.Store
	logger   *slog.Logger
	dec      *zstd.Decoder
}

// NewConsumer builds a Consumer. cur may be nil to disable reading a persisted
// start cursor. If Config.Zstd is set, the embedded dictionary must be non-empty.
func NewConsumer(cfg Config, p *Pipeline, m *metrics.Metrics, cur *cursor.Store) (*Consumer, error) {
	if cfg.Dial == nil {
		cfg.Dial = dialWebSocket
	}
	if cfg.Sleep == nil {
		cfg.Sleep = sleepCtx
	}
	if cfg.Jitter == nil {
		cfg.Jitter = rand.Float64
	}
	if cfg.BackoffBase <= 0 {
		cfg.BackoffBase = time.Second
	}
	if cfg.BackoffMax <= 0 {
		cfg.BackoffMax = 30 * time.Second
	}
	if len(cfg.Hosts) == 0 {
		return nil, errors.New("jetstream: no hosts configured")
	}

	c := &Consumer{cfg: cfg, pipeline: p, m: m, cur: cur, logger: slog.Default()}
	if cfg.Zstd {
		if len(zstdDict) == 0 {
			return nil, errors.New("jetstream: zstd enabled but embedded dictionary is empty (run make update-zstd-dict)")
		}
		dec, err := zstd.NewReader(nil, zstd.WithDecoderDicts(zstdDict))
		if err != nil {
			return nil, err
		}
		c.dec = dec
	}
	return c, nil
}

// Run connects and consumes until ctx is cancelled, reconnecting as needed.
// It always returns a non-nil error (ctx.Err() on clean shutdown).
func (c *Consumer) Run(ctx context.Context) error {
	bo := &backoffState{base: c.cfg.BackoffBase, max: c.cfg.BackoffMax, factor: 2, jitter: c.cfg.Jitter}
	hostIdx := 0

	// Read the persisted start cursor once; reconnects use the live LastTimeUS.
	var persisted int64
	if c.cur != nil {
		if v, err := c.cur.ReadInt64(0); err == nil {
			persisted = v
		}
	}

	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		host := c.cfg.Hosts[hostIdx%len(c.cfg.Hosts)]
		u := buildURL(host, c.cfg.Collections, c.startCursor(persisted), c.cfg.Zstd)

		conn, err := c.cfg.Dial(ctx, u)
		if err != nil {
			c.m.SetWSConnected(false)
			c.logger.Warn("jetstream dial failed", "host", host, "err", err)
			if reErr := c.reconnectWait(ctx, bo, &hostIdx); reErr != nil {
				return reErr
			}
			continue
		}

		c.m.SetWSConnected(true)
		bo.reset()
		c.logger.Info("jetstream connected", "host", host)

		readErr := c.readLoop(ctx, conn)
		_ = conn.Close()
		c.m.SetWSConnected(false)

		if err := ctx.Err(); err != nil {
			return err
		}
		c.logger.Warn("jetstream disconnected", "host", host, "err", readErr)
		if reErr := c.reconnectWait(ctx, bo, &hostIdx); reErr != nil {
			return reErr
		}
	}
}

// startCursor returns the rewound cursor to request: the live LastTimeUS once
// we've processed anything, otherwise the persisted cursor.
func (c *Consumer) startCursor(persisted int64) int64 {
	if last := c.pipeline.LastTimeUS(); last > 0 {
		return cursor.RewindMicros(last, c.cfg.Rewind)
	}
	return cursor.RewindMicros(persisted, c.cfg.Rewind)
}

func (c *Consumer) reconnectWait(ctx context.Context, bo *backoffState, hostIdx *int) error {
	c.m.IncWSReconnects()
	*hostIdx++
	return c.cfg.Sleep(ctx, bo.next())
}

func (c *Consumer) readLoop(ctx context.Context, conn wsConn) error {
	for {
		data, err := conn.Read(ctx)
		if err != nil {
			return err
		}
		wire := len(data)
		if c.dec != nil {
			decoded, derr := c.dec.DecodeAll(data, nil)
			if derr != nil {
				c.m.AddWSBytes(wire, 0)
				c.logger.Warn("jetstream zstd decode failed", "err", derr)
				continue
			}
			data = decoded
		}
		c.m.AddWSBytes(wire, len(data))
		if err := c.pipeline.Process(data); err != nil {
			c.logger.Debug("jetstream message decode failed", "err", err)
			continue
		}
	}
}

// buildURL constructs a Jetstream /subscribe URL with the requested collections,
// cursor (omitted when 0), and compression flag.
func buildURL(host string, collections []string, cur int64, useZstd bool) string {
	v := url.Values{}
	for _, coll := range collections {
		v.Add("wantedCollections", coll)
	}
	if cur > 0 {
		v.Set("cursor", strconv.FormatInt(cur, 10))
	}
	if useZstd {
		v.Set("compress", "true")
	}
	return "wss://" + host + "/subscribe?" + v.Encode()
}

// backoffState implements bounded exponential backoff with full jitter.
type backoffState struct {
	base    time.Duration
	max     time.Duration
	factor  float64
	attempt int
	jitter  func() float64
}

func (b *backoffState) next() time.Duration {
	d := float64(b.base) * math.Pow(b.factor, float64(b.attempt))
	if d > float64(b.max) {
		d = float64(b.max)
	}
	b.attempt++
	// Full jitter: uniformly in [d/2, d].
	return time.Duration(d*0.5 + d*0.5*b.jitter())
}

func (b *backoffState) reset() { b.attempt = 0 }

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// dialWebSocket is the production DialFunc, wrapping coder/websocket.
func dialWebSocket(ctx context.Context, u string) (wsConn, error) {
	conn, _, err := websocket.Dial(ctx, u, nil)
	if err != nil {
		return nil, err
	}
	conn.SetReadLimit(readLimit)
	return &coderConn{conn: conn}, nil
}

type coderConn struct {
	conn *websocket.Conn
}

func (cc *coderConn) Read(ctx context.Context) ([]byte, error) {
	_, data, err := cc.conn.Read(ctx)
	return data, err
}

func (cc *coderConn) Close() error {
	return cc.conn.Close(websocket.StatusNormalClosure, "")
}
