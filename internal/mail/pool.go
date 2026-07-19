package mail

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/recloud/mailer/internal/config"
)

type Pool struct {
	mu    sync.Mutex
	conns map[string]*imapclient.Client
	done  chan struct{}
}

func NewPool(noopInterval time.Duration) *Pool {
	p := &Pool{
		conns: make(map[string]*imapclient.Client),
		done:  make(chan struct{}),
	}
	if noopInterval > 0 {
		go p.keepAlive(noopInterval)
	}
	return p
}

func (p *Pool) Acquire(ctx context.Context, acc config.Account) (*imapclient.Client, error) {
	p.mu.Lock()
	client, ok := p.conns[acc.Name]
	if ok {
		delete(p.conns, acc.Name)
	}
	p.mu.Unlock()

	if ok {
		if err := client.Noop().Wait(); err == nil {
			return client, nil
		}
		client.Close()
	}
	return dial(ctx, acc)
}

func (p *Pool) Release(acc string, client *imapclient.Client) {
	p.mu.Lock()
	p.conns[acc] = client
	p.mu.Unlock()
}

func (p *Pool) Close() {
	close(p.done)
	p.mu.Lock()
	defer p.mu.Unlock()
	for name, client := range p.conns {
		func() {
			_ = client.Noop().Wait()
			client.Logout().Wait()
			client.Close()
		}()
		delete(p.conns, name)
	}
}

func (p *Pool) keepAlive(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-p.done:
			return
		case <-ticker.C:
			p.noopAll()
		}
	}
}

func (p *Pool) noopAll() {
	p.mu.Lock()
	snapshot := make(map[string]*imapclient.Client, len(p.conns))
	for k, v := range p.conns {
		snapshot[k] = v
	}
	p.mu.Unlock()

	for name, client := range snapshot {
		done := make(chan struct{}, 1)
		go func() {
			client.Noop().Wait()
			done <- struct{}{}
		}()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
			p.mu.Lock()
			delete(p.conns, name)
			p.mu.Unlock()
			client.Close()
			log.Printf("[pool] %s: noop timeout, removed", name)
		}
	}
}
