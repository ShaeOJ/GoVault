package node

import (
	"fmt"
	"sync"
	"time"
)

type ChainMonitor struct {
	client        *Client
	lastBlockHash string
	pollInterval  time.Duration
	refreshInterval time.Duration
	gbtRules      []string

	OnNewBlock        func(*BlockTemplate)
	OnTemplateRefresh func(*BlockTemplate)
	onError           func(error)

	stopCh chan struct{}
	wg     sync.WaitGroup
}

func NewChainMonitor(client *Client, pollInterval time.Duration, gbtRules []string) *ChainMonitor {
	if pollInterval == 0 {
		pollInterval = 500 * time.Millisecond
	}
	return &ChainMonitor{
		client:       client,
		pollInterval: pollInterval,
		gbtRules:     gbtRules,
		stopCh:       make(chan struct{}),
	}
}

func (m *ChainMonitor) SetRefreshInterval(d time.Duration) {
	m.refreshInterval = d
}

func (m *ChainMonitor) SetOnError(fn func(error)) {
	m.onError = fn
}

func (m *ChainMonitor) Start() {
	m.wg.Add(1)
	go m.pollLoop()
}

func (m *ChainMonitor) Stop() {
	close(m.stopCh)
	m.wg.Wait()
}

func (m *ChainMonitor) pollLoop() {
	defer m.wg.Done()

	blockTicker := time.NewTicker(m.pollInterval)
	defer blockTicker.Stop()

	// Optional periodic template refresh (gives miners fresh ntime/search space)
	var refreshCh <-chan time.Time
	if m.refreshInterval > 0 {
		refreshTicker := time.NewTicker(m.refreshInterval)
		defer refreshTicker.Stop()
		refreshCh = refreshTicker.C
	}

	// Do an initial check immediately
	m.checkNewBlock()

	for {
		select {
		case <-m.stopCh:
			return
		case <-blockTicker.C:
			m.checkNewBlock()
		case <-refreshCh:
			m.refreshCurrentTemplate()
		}
	}
}

func (m *ChainMonitor) refreshCurrentTemplate() {
	if m.OnTemplateRefresh == nil {
		return
	}
	tmpl, err := m.client.GetBlockTemplate(m.gbtRules)
	if err != nil {
		if m.onError != nil {
			m.onError(fmt.Errorf("refresh template: %w", err))
		}
		return
	}
	m.OnTemplateRefresh(tmpl)
}

func (m *ChainMonitor) checkNewBlock() {
	hash, err := m.client.GetBestBlockHash()
	if err != nil {
		if m.onError != nil {
			m.onError(fmt.Errorf("getbestblockhash: %w", err))
		}
		return
	}

	if hash == m.lastBlockHash {
		return
	}

	m.lastBlockHash = hash

	if m.OnNewBlock == nil {
		return
	}

	tmpl, err := m.client.GetBlockTemplate(m.gbtRules)
	if err != nil {
		if m.onError != nil {
			m.onError(fmt.Errorf("getblocktemplate: %w", err))
		}
		return
	}

	m.OnNewBlock(tmpl)
}

func (m *ChainMonitor) RefreshTemplate() (*BlockTemplate, error) {
	return m.client.GetBlockTemplate(m.gbtRules)
}
