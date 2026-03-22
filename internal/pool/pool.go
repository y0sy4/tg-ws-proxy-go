// Package pool provides WebSocket connection pooling.
package pool

import (
	"sync"
	"time"

	"github.com/Flowseal/tg-ws-proxy/internal/websocket"
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
	mu        sync.Mutex
	idle      map[DCKey][]*pooledWS
	refilling map[DCKey]bool
	poolSize  int
	maxAge    time.Duration
}

func NewWSPool(poolSize int, maxAge time.Duration) *WSPool {
	if poolSize <= 0 {
		poolSize = DefaultPoolSize
	}
	if maxAge <= 0 {
		maxAge = DefaultMaxAge
	}
	return &WSPool{
		idle:      make(map[DCKey][]*pooledWS),
		refilling: make(map[DCKey]bool),
		poolSize:  poolSize,
		maxAge:    maxAge,
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

		if age > p.maxAge || pws.ws == nil {
			if pws.ws != nil {
				pws.ws.Close()
			}
			continue
		}

		p.idle[key] = bucket
		p.scheduleRefill(key)
		return pws.ws
	}

	p.idle[key] = bucket
	p.scheduleRefill(key)
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

func (p *WSPool) scheduleRefill(key DCKey) {
	if p.refilling[key] {
		return
	}
	p.refilling[key] = true
}

func (p *WSPool) NeedRefill(key DCKey) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.idle[key]) < p.poolSize
}

func (p *WSPool) SetRefilling(key DCKey, refilling bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.refilling[key] = refilling
}
