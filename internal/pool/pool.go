// Package pool provides WebSocket connection pooling.
package pool

import (
	"sync"
	"time"

	"github.com/y0sy4/telegram-proxy/internal/websocket"
)

const (
	DefaultPoolSize = 4
	DefaultMaxAge   = 120 * time.Second
)

type DCKey struct {
	DC      int
	IsMedia bool
}

type pooledWS struct {
	ws      *websocket.WebSocket
	created time.Time
}

type WSPool struct {
	mu       sync.Mutex
	idle     map[DCKey][]*pooledWS
	poolSize int
	maxAge   time.Duration
}

func NewWSPool(poolSize int, maxAge time.Duration) *WSPool {
	if poolSize <= 0 {
		poolSize = DefaultPoolSize
	}
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	return &WSPool{
		idle:     make(map[DCKey][]*pooledWS),
		poolSize: poolSize,
		maxAge:   maxAge,
	}
}

func (p *WSPool) Get(key DCKey) *websocket.WebSocket {
	p.mu.Lock()
	defer p.mu.Unlock()

	bucket := p.idle[key]
	now := time.Now()

	for len(bucket) > 0 {
		pws := bucket[0]
		bucket = bucket[1:]
		age := now.Sub(pws.created)

		if age > p.maxAge || pws.ws == nil || pws.ws.IsClosed() {
			if pws.ws != nil {
				pws.ws.Close()
			}
			continue
		}

		p.idle[key] = bucket
		return pws.ws
	}

	p.idle[key] = bucket
	return nil
}

func (p *WSPool) Put(key DCKey, ws *websocket.WebSocket) {
	p.mu.Lock()
	defer p.mu.Unlock()

	p.idle[key] = append(p.idle[key], &pooledWS{
		ws:      ws,
		created: time.Now(),
	})
}

func (p *WSPool) NeedRefill(key DCKey) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle[key]) < p.poolSize
}
