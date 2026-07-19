package app

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/recloud/mailer/internal/config"
	"github.com/recloud/mailer/internal/mail"
	"github.com/recloud/mailer/internal/notify"
	"github.com/recloud/mailer/internal/state"
)

const pollTimeout = 30 * time.Second

type Poller struct {
	cfg       *config.Config
	store     *state.Store
	pool      *mail.Pool
	notifiers []notify.Notifier
	accNotifs map[string][]string
	firstRun  map[string]bool
	mu        sync.Mutex
	metrics   *Metrics
}

func New(cfg *config.Config, store *state.Store) (*Poller, error) {
	pool := mail.NewPool(cfg.NoopInterval)

	notifMap := make(map[string][]string)
	for _, a := range cfg.Accounts {
		if len(a.Notifiers) == 0 {
			continue
		}
		notifMap[a.Name] = a.Notifiers
	}

	var notifiers []notify.Notifier
	if cfg.Telegram.Enabled {
		notifiers = append(notifiers, notify.NewTelegram(cfg.Telegram))
	}
	if cfg.Discord.Enabled {
		discord, err := notify.NewDiscord(cfg.Discord, cfg.Accounts)
		if err != nil {
			pool.Close()
			return nil, err
		}
		notifiers = append(notifiers, discord)
	}

	firstRun := make(map[string]bool, len(cfg.Accounts))
	for _, a := range cfg.Accounts {
		firstRun[a.Name] = true
	}

	return &Poller{
		cfg:       cfg,
		store:     store,
		pool:      pool,
		notifiers: notifiers,
		accNotifs: notifMap,
		firstRun:  firstRun,
		metrics:   NewMetrics(),
	}, nil
}

func (p *Poller) Run(ctx context.Context) {
	log.Printf("mailer started: %d account(s), interval %s, notifiers %v",
		len(p.cfg.Accounts), p.cfg.PollInterval, p.notifierNames())

	p.pollAll(ctx)

	ticker := time.NewTicker(p.cfg.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Print("shutting down")
			return
		case <-ticker.C:
			p.pollAll(ctx)
		}
	}
}

func (p *Poller) Metrics() *Metrics {
	return p.metrics
}

func (p *Poller) Close() {
	p.pool.Close()
}

func (p *Poller) pollAll(ctx context.Context) {
	var wg sync.WaitGroup
	for _, acc := range p.cfg.Accounts {
		wg.Add(1)
		go func(acc config.Account) {
			defer wg.Done()
			pollCtx, cancel := context.WithTimeout(ctx, pollTimeout)
			defer cancel()
			start := time.Now()
			if err := p.pollAccount(pollCtx, acc); err != nil {
				log.Printf("[%s] poll error: %v", acc.Name, err)
			}
			p.metrics.Observe("mailer_poll_duration_seconds",
				map[string]string{"account": acc.Name},
				time.Since(start).Seconds(),
			)
		}(acc)
	}
	wg.Wait()
}

func (p *Poller) pollAccount(ctx context.Context, acc config.Account) error {
	prev, _ := p.store.Get(acc.Name)

	p.mu.Lock()
	first := p.firstRun[acc.Name]
	p.firstRun[acc.Name] = false
	p.mu.Unlock()

	client, err := p.pool.Acquire(ctx, acc)
	if err != nil {
		return err
	}
	defer p.pool.Release(acc.Name, client)

	res, err := mail.Fetch(ctx, client, acc, prev.LastUID, first, prev.UIDValidity)
	if err != nil {
		var changed *mail.UIDValidityChanged
		if errors.As(err, &changed) {
			log.Printf("[%s] %v, resetting baseline", acc.Name, changed)
			return p.store.Set(acc.Name, state.AccountState{UIDValidity: changed.Current})
		}
		return err
	}

	p.metrics.Add("mailer_messages_fetched_total",
		map[string]string{"account": acc.Name},
		int64(len(res.Messages)),
	)

	newState := state.AccountState{LastUID: res.HighestUID, UIDValidity: res.UIDValidity}

	if len(res.Messages) == 0 {
		if newState != prev {
			return p.store.Set(acc.Name, newState)
		}
		return nil
	}

	log.Printf("[%s] %d new message(s)", acc.Name, len(res.Messages))

	notifiers := p.notifiersFor(acc)
	titleTmpl := p.templateFor("title", acc)
	textTmpl := p.templateFor("text", acc)

	var delivered []uint32
	for i := range res.Messages {
		msg := &res.Messages[i]
		msg.TitleTmpl = titleTmpl
		msg.TextTmpl = textTmpl

		dup, err := p.store.IsDuplicate(acc.Name, msg.MessageID)
		if err != nil {
			log.Printf("[%s] dedup check: %v", acc.Name, err)
		} else if dup {
			log.Printf("[%s] skip duplicate: %s", acc.Name, msg.MessageID)
			continue
		}

		if p.dispatch(ctx, *msg, notifiers) {
			delivered = append(delivered, msg.UID)
			if err := p.store.MarkDelivered(acc.Name, msg.MessageID); err != nil {
				log.Printf("[%s] mark delivered: %v", acc.Name, err)
			}
		}
	}

	if len(delivered) > 0 {
		newState.LastUID = maxUID(delivered)
	}
	if err := p.store.Set(acc.Name, newState); err != nil {
		return err
	}

	if acc.MarkSeen && len(delivered) > 0 {
		if err := mail.MarkSeen(ctx, client, acc, delivered); err != nil {
			log.Printf("[%s] mark seen: %v", acc.Name, err)
		}
	}
	return nil
}

func (p *Poller) notifiersFor(acc config.Account) []notify.Notifier {
	if names, ok := p.accNotifs[acc.Name]; ok {
		filtered := make([]notify.Notifier, 0, len(names))
		for _, n := range p.notifiers {
			for _, name := range names {
				if n.Name() == name {
					filtered = append(filtered, n)
					break
				}
			}
		}
		return filtered
	}
	return p.notifiers
}

func (p *Poller) templateFor(field string, acc config.Account) string {
	if acc.MessageTemplate != nil {
		switch field {
		case "title":
			return acc.MessageTemplate.Title
		case "text":
			return acc.MessageTemplate.Text
		}
	}
	if p.cfg.MessageTemplate != nil {
		switch field {
		case "title":
			return p.cfg.MessageTemplate.Title
		case "text":
			return p.cfg.MessageTemplate.Text
		}
	}
	return ""
}

func maxUID(uids []uint32) uint32 {
	var max uint32
	for _, u := range uids {
		if u > max {
			max = u
		}
	}
	return max
}

func (p *Poller) dispatch(ctx context.Context, msg mail.Message, notifiers []notify.Notifier) bool {
	ok := false
	for _, n := range notifiers {
		for attempt := 0; attempt <= p.cfg.RetryAttempts; attempt++ {
			if attempt > 0 {
				delay := p.cfg.RetryDelay * (1 << (attempt - 1))
				select {
				case <-ctx.Done():
					return ok
				case <-time.After(delay):
				}
			}
			if err := n.Send(ctx, msg); err != nil {
				if attempt < p.cfg.RetryAttempts {
					log.Printf("[%s] %s notify error (retry %d/%d): %v",
						msg.Account, n.Name(), attempt+1, p.cfg.RetryAttempts, err)
				} else {
					log.Printf("[%s] %s notify error (failed after %d attempts): %v",
						msg.Account, n.Name(), p.cfg.RetryAttempts+1, err)
				}
				continue
			}
			p.metrics.Inc("mailer_messages_delivered_total",
				map[string]string{"account": msg.Account, "notifier": n.Name()},
			)
			ok = true
			break
		}
	}
	return ok
}

func (p *Poller) notifierNames() []string {
	names := make([]string, 0, len(p.notifiers))
	for _, n := range p.notifiers {
		names = append(names, n.Name())
	}
	return names
}
