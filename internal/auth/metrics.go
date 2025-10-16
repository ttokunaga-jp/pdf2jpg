package auth

import (
	"expvar"
	"fmt"
	"sync"
)

// metricsRecorder centralises counter/gauge updates so the rest of the package stays testable.
type metricsRecorder interface {
	IncKeyIssue(result, operator string)
	IncKeyValidation(outcome validationOutcome)
	SetTemporaryKeysActive(count int)
}

type expvarMetrics struct {
	issueMap      *expvar.Map
	validationMap *expvar.Map
	activeGauge   *expvar.Int
	mu            sync.Mutex
}

func newExpvarMetrics() *expvarMetrics {
	return &expvarMetrics{
		issueMap:      ensureExpvarMap("api_key_issue_total"),
		validationMap: ensureExpvarMap("api_key_validation_total"),
		activeGauge:   ensureExpvarInt("temporary_keys_active"),
	}
}

func (m *expvarMetrics) IncKeyIssue(result, operator string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf(`{"result":"%s","operator":"%s"}`, result, operator)
	current := getExpvarInt(m.issueMap, key)
	current.Add(1)
}

func (m *expvarMetrics) IncKeyValidation(outcome validationOutcome) {
	m.mu.Lock()
	defer m.mu.Unlock()
	key := fmt.Sprintf(`{"outcome":"%s"}`, outcome)
	current := getExpvarInt(m.validationMap, key)
	current.Add(1)
}

func (m *expvarMetrics) SetTemporaryKeysActive(count int) {
	m.activeGauge.Set(int64(count))
}

func getExpvarInt(m *expvar.Map, key string) *expvar.Int {
	if existing := m.Get(key); existing != nil {
		if intVar, ok := existing.(*expvar.Int); ok {
			return intVar
		}
	}
	intVar := new(expvar.Int)
	m.Set(key, intVar)
	return intVar
}

func ensureExpvarMap(name string) *expvar.Map {
	if existing := expvar.Get(name); existing != nil {
		if m, ok := existing.(*expvar.Map); ok {
			return m
		}
	}
	return expvar.NewMap(name)
}

func ensureExpvarInt(name string) *expvar.Int {
	if existing := expvar.Get(name); existing != nil {
		if v, ok := existing.(*expvar.Int); ok {
			return v
		}
	}
	return expvar.NewInt(name)
}
