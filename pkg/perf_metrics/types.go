package perfmetrics

import (
	"math"
	"sync"
)

type Store interface {
	Record(sample Sample)
	Query(params QueryParams) (QueryResult, error)
}

type Sample struct {
	Model        string
	Group        string
	LatencyMs    int64
	TtftMs       int64
	HasTtft      bool
	Success      bool
	OutputTokens int64
	GenerationMs int64
}

type QueryParams struct {
	Model string
	Group string
	Hours int
	// AllowedGroups restricts both the per-group results and the model summary;
	// nil allows every group.
	AllowedGroups []string
}

// Summary is the request-weighted aggregate over every bucket in the window.
type Summary struct {
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
}

type BucketPoint struct {
	Ts           int64   `json:"ts"`
	AvgTtftMs    int64   `json:"avg_ttft_ms"`
	AvgLatencyMs int64   `json:"avg_latency_ms"`
	SuccessRate  float64 `json:"success_rate"`
	AvgTps       float64 `json:"avg_tps"`
}

type GroupResult struct {
	Group        string        `json:"group"`
	AvgTtftMs    int64         `json:"avg_ttft_ms"`
	AvgLatencyMs int64         `json:"avg_latency_ms"`
	SuccessRate  float64       `json:"success_rate"`
	AvgTps       float64       `json:"avg_tps"`
	Series       []BucketPoint `json:"series"`
}

type QueryResult struct {
	ModelName    string        `json:"model_name"`
	SeriesSchema string        `json:"series_schema"`
	Summary      *Summary      `json:"summary"`
	Series       []BucketPoint `json:"series"`
	WindowStart  int64         `json:"window_start"`
	WindowEnd    int64         `json:"window_end"`
	Groups       []GroupResult `json:"groups"`
}

type SuccessRatePoint struct {
	Ts          int64   `json:"ts"`
	SuccessRate float64 `json:"success_rate"`
}

type ModelSummary struct {
	ModelName           string             `json:"model_name"`
	AvgLatencyMs        int64              `json:"avg_latency_ms"`
	SuccessRate         float64            `json:"success_rate"`
	AvgTps              float64            `json:"avg_tps"`
	RecentSuccessSeries []SuccessRatePoint `json:"recent_success_series,omitempty"`
	RequestCount        int64              `json:"-"`
}

type SummaryAllResult struct {
	Summary     *Summary       `json:"summary"`
	WindowStart int64          `json:"window_start"`
	WindowEnd   int64          `json:"window_end"`
	Models      []ModelSummary `json:"models"`
	// RecentSuccessSeries contains up to 24 nonempty hours from the last 7 days,
	// including requested models with no traffic in the summary window.
	RecentSuccessSeries map[string][]SuccessRatePoint `json:"recent_success_series,omitempty"`
}

// AvailabilityHours is the number of hourly cells in an availability report.
const AvailabilityHours = 24

// AvailabilityStats aggregates one report row. The hourly slices hold
// AvailabilityHours entries, oldest first, starting at the report's WindowStart.
type AvailabilityStats struct {
	RequestCount    int64   `json:"request_count"`
	SuccessRate     float64 `json:"success_rate"`
	AvgLatencyMs    int64   `json:"avg_latency_ms"`
	AvgTps          float64 `json:"avg_tps"`
	HourlyRequests  []int64 `json:"hourly_requests"`
	HourlySuccesses []int64 `json:"hourly_successes"`
}

type AvailabilityModel struct {
	ModelName string `json:"model_name"`
	AvailabilityStats
	Groups []AvailabilityGroup `json:"groups,omitempty"`
}

type AvailabilityGroup struct {
	Group string `json:"group"`
	AvailabilityStats
	Models []AvailabilityModel `json:"models,omitempty"`
}

type AvailabilityResult struct {
	WindowStart int64               `json:"window_start"`
	WindowEnd   int64               `json:"window_end"`
	Summary     AvailabilityStats   `json:"summary"`
	Groups      []AvailabilityGroup `json:"groups"`
	Models      []AvailabilityModel `json:"models"`
}

type availabilityTotals struct {
	total counters
	hours [AvailabilityHours]counters
}

func (t *availabilityTotals) add(hour int, value counters) {
	for _, target := range []*counters{&t.total, &t.hours[hour]} {
		target.requestCount += value.requestCount
		target.successCount += value.successCount
		target.totalLatencyMs += value.totalLatencyMs
		target.outputTokens += value.outputTokens
		target.generationMs += value.generationMs
	}
}

func (t *availabilityTotals) stats() AvailabilityStats {
	stats := AvailabilityStats{
		RequestCount:    t.total.requestCount,
		SuccessRate:     math.Round(successRate(t.total)*100) / 100,
		AvgLatencyMs:    avg(t.total.totalLatencyMs, t.total.requestCount),
		AvgTps:          math.Round(avgTps(t.total)*100) / 100,
		HourlyRequests:  make([]int64, AvailabilityHours),
		HourlySuccesses: make([]int64, AvailabilityHours),
	}
	for i, hour := range t.hours {
		stats.HourlyRequests[i] = hour.requestCount
		stats.HourlySuccesses[i] = hour.successCount
	}
	return stats
}

type bucketKey struct {
	model    string
	group    string
	bucketTs int64
}

type counters struct {
	requestCount   int64
	successCount   int64
	totalLatencyMs int64
	ttftSumMs      int64
	ttftCount      int64
	outputTokens   int64
	generationMs   int64
}

// atomicBucket accumulates one model/group/time bucket. A mutex guards the
// counters so that every sample, and every drain, covers all of them at once:
// a flush of the running bucket can never split one sample across batches.
type atomicBucket struct {
	mu       sync.Mutex
	counters counters
}

func (b *atomicBucket) add(sample Sample) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counters.requestCount++
	if sample.Success {
		b.counters.successCount++
	}
	if sample.LatencyMs > 0 {
		b.counters.totalLatencyMs += sample.LatencyMs
	}
	if sample.HasTtft && sample.TtftMs >= 0 {
		b.counters.ttftSumMs += sample.TtftMs
		b.counters.ttftCount++
	}
	if sample.OutputTokens > 0 && sample.GenerationMs > 0 {
		b.counters.outputTokens += sample.OutputTokens
		b.counters.generationMs += sample.GenerationMs
	}
}

func (b *atomicBucket) snapshot() counters {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.counters
}

// drain returns the counters and resets them in one step.
func (b *atomicBucket) drain() counters {
	b.mu.Lock()
	defer b.mu.Unlock()
	drained := b.counters
	b.counters = counters{}
	return drained
}

func (b *atomicBucket) addCounters(c counters) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.counters.requestCount += c.requestCount
	b.counters.successCount += c.successCount
	b.counters.totalLatencyMs += c.totalLatencyMs
	b.counters.ttftSumMs += c.ttftSumMs
	b.counters.ttftCount += c.ttftCount
	b.counters.outputTokens += c.outputTokens
	b.counters.generationMs += c.generationMs
}
