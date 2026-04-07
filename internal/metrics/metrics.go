package metrics

import (
	"fmt"
	"strings"
	"sync"
	"time"
)

type Collector struct {
	mu                    sync.RWMutex
	transactionsReceived  int64
	transactionsSucceeded int64
	transactionsFailed    int64
	transactionsFlagged   int64
	totalLatency          time.Duration
	latencySamples        int64
}

func NewCollector() *Collector {
	return &Collector{}
}

func (c *Collector) IncReceived() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transactionsReceived++
}

func (c *Collector) IncSucceeded(latency time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transactionsSucceeded++
	c.totalLatency += latency
	c.latencySamples++
}

func (c *Collector) IncFailed(latency time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transactionsFailed++
	c.totalLatency += latency
	c.latencySamples++
}

func (c *Collector) IncFlagged() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.transactionsFlagged++
}

func (c *Collector) Snapshot() map[string]float64 {
	c.mu.RLock()
	defer c.mu.RUnlock()

	avgMs := 0.0
	if c.latencySamples > 0 {
		avgMs = float64(c.totalLatency.Milliseconds()) / float64(c.latencySamples)
	}

	return map[string]float64{
		"transactions_received_total":       float64(c.transactionsReceived),
		"transactions_processed_successful": float64(c.transactionsSucceeded),
		"transactions_processed_failed":     float64(c.transactionsFailed),
		"transactions_flagged_total":        float64(c.transactionsFlagged),
		"processing_latency_avg_ms":         avgMs,
	}
}

func (c *Collector) RenderPrometheus() string {
	snapshot := c.Snapshot()
	lines := make([]string, 0, len(snapshot))
	for key, value := range snapshot {
		lines = append(lines, fmt.Sprintf("%s %v", key, value))
	}
	return strings.Join(lines, "\n") + "\n"
}
