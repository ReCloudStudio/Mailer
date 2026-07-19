package app

import (
	"fmt"
	"strings"
	"sync"
)

type Metrics struct {
	mu       sync.Mutex
	counters map[string]int64
	gauges   map[string]float64
}

func NewMetrics() *Metrics {
	return &Metrics{
		counters: make(map[string]int64),
		gauges:   make(map[string]float64),
	}
}

func (m *Metrics) Inc(name string, labels map[string]string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(name, labels)
	m.counters[key]++
}

func (m *Metrics) Add(name string, labels map[string]string, delta int64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(name, labels)
	m.counters[key] += delta
}

func (m *Metrics) Observe(name string, labels map[string]string, val float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := metricKey(name, labels)
	m.gauges[key] = val
}

func (m *Metrics) Snapshot() string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var b strings.Builder
	for key, val := range m.counters {
		name, labels := splitMetricKey(key)
		if labels != "" {
			fmt.Fprintf(&b, "# HELP %s Total count\n# TYPE %s counter\n%s{%s} %d\n", name, name, name, labels, val)
		} else {
			fmt.Fprintf(&b, "# HELP %s Total count\n# TYPE %s counter\n%s %d\n", name, name, name, val)
		}
	}
	for key, val := range m.gauges {
		name, labels := splitMetricKey(key)
		if labels != "" {
			fmt.Fprintf(&b, "# HELP %s Current value\n# TYPE %s gauge\n%s{%s} %v\n", name, name, name, labels, val)
		} else {
			fmt.Fprintf(&b, "# HELP %s Current value\n# TYPE %s gauge\n%s %v\n", name, name, name, val)
		}
	}
	return b.String()
}

func metricKey(name string, labels map[string]string) string {
	if len(labels) == 0 {
		return name + "|"
	}
	parts := make([]string, 0, len(labels))
	for k, v := range labels {
		parts = append(parts, fmt.Sprintf("%s=%q", k, v))
	}
	return name + "|" + strings.Join(parts, ",")
}

func splitMetricKey(key string) (string, string) {
	idx := strings.IndexByte(key, '|')
	if idx < 0 {
		return key, ""
	}
	return key[:idx], key[idx+1:]
}
