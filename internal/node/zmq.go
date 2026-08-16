package node

import (
	"context"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-zeromq/zmq4"
)

// ZMQSubscriber listens to a Bitcoin Core ZMQ PUB socket for "hashblock"
// notifications and fires OnNewHash each time a new block is seen. It
// reconnects automatically if the connection drops. Uses the pure-Go
// go-zeromq/zmq4 implementation, so there is no libzmq/CGO dependency and the
// binary stays statically cross-compilable (important for the relay-mk1 /
// Buildroot targets).
type ZMQSubscriber struct {
	endpoint  string
	connected atomic.Bool

	// OnNewHash is called with the hex block hash (display byte order) each
	// time a new block arrives.
	OnNewHash func(hash string)

	onError func(error)

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewZMQSubscriber creates a subscriber that will connect to endpoint
// (e.g. "tcp://10.0.0.114:28332") and listen for hashblock notifications.
func NewZMQSubscriber(endpoint string) *ZMQSubscriber {
	return &ZMQSubscriber{endpoint: endpoint}
}

// SetOnError sets an optional error handler called on connection or receive failures.
func (z *ZMQSubscriber) SetOnError(fn func(error)) { z.onError = fn }

// IsConnected reports whether the subscriber currently has an active connection.
func (z *ZMQSubscriber) IsConnected() bool { return z.connected.Load() }

// Start launches the subscriber goroutine. Returns immediately.
func (z *ZMQSubscriber) Start() {
	ctx, cancel := context.WithCancel(context.Background())
	z.cancel = cancel
	z.wg.Add(1)
	go z.loop(ctx)
}

// Stop shuts down the subscriber and waits for the goroutine to exit.
func (z *ZMQSubscriber) Stop() {
	if z.cancel != nil {
		z.cancel()
	}
	z.wg.Wait()
}

func (z *ZMQSubscriber) loop(ctx context.Context) {
	defer z.wg.Done()

	for {
		if ctx.Err() != nil {
			return
		}
		if err := z.run(ctx); err != nil && ctx.Err() == nil {
			if z.onError != nil {
				z.onError(fmt.Errorf("zmq: %w — reconnecting in 5s", err))
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
		}
	}
}

func (z *ZMQSubscriber) run(ctx context.Context) error {
	sock := zmq4.NewSub(ctx, zmq4.WithDialerRetry(time.Second))
	defer func() {
		z.connected.Store(false)
		sock.Close()
	}()

	if err := sock.Dial(z.endpoint); err != nil {
		return fmt.Errorf("dial %s: %w", z.endpoint, err)
	}
	if err := sock.SetOption(zmq4.OptionSubscribe, "hashblock"); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	z.connected.Store(true)

	for {
		msg, err := sock.Recv()
		if err != nil {
			if ctx.Err() != nil {
				return nil // clean shutdown
			}
			return fmt.Errorf("recv: %w", err)
		}
		// Bitcoin Core ZMQ hashblock message has 3 frames:
		//   [0] topic: "hashblock"
		//   [1] 32-byte block hash (binary, little-endian internal order)
		//   [2] 4-byte sequence number (uint32 LE)
		if len(msg.Frames) < 2 {
			continue
		}
		if string(msg.Frames[0]) != "hashblock" {
			continue
		}
		hashBytes := msg.Frames[1]
		if len(hashBytes) != 32 {
			continue
		}
		// Bitcoin hashes are shown reversed (big-endian display order). ZMQ
		// delivers internal (little-endian) order, so reverse for display.
		// NOTE: this is the opposite byte order to getbestblockhash, which is
		// why the poll fallback dedups by height, not by comparing hashes.
		rev := make([]byte, 32)
		for i, b := range hashBytes {
			rev[31-i] = b
		}
		if z.OnNewHash != nil {
			z.OnNewHash(hex.EncodeToString(rev))
		}
	}
}
