// Package metrics owns the Prometheus registry and every collector in the
// exporter's metrics contract (see SPEC.md). All label-bearing metrics are
// reached through façade methods that accept already-normalized values, and the
// top-N gauges are wrapped so that series falling out of the top set are deleted
// — together these keep cardinality bounded.
package metrics

import (
	"anto.pt/x/atproto-exporter/internal/topn"

	"github.com/prometheus/client_golang/prometheus"
)

// Metrics holds all collectors. Construct with New; it registers everything on
// the provided registry.
type Metrics struct {
	eventsTotal     *prometheus.CounterVec
	postsTotal      *prometheus.CounterVec
	firehoseLag     prometheus.Gauge
	eventsProcessed prometheus.Counter

	plcOperations *prometheus.CounterVec
	federationPDS prometheus.Gauge

	topDomains  *prometheus.GaugeVec
	topHashtags *prometheus.GaugeVec

	// TopDomains and TopHashtags apply a topn.Snapshot to their gauge vector,
	// deleting evicted series so cardinality never exceeds N.
	TopDomains  *TopNGauge
	TopHashtags *TopNGauge

	wsConnected     prometheus.Gauge
	wsReconnects    prometheus.Counter
	plcPollErrors   prometheus.Counter
	plcPollDuration prometheus.Histogram
}

// New builds and registers every collector on reg.
func New(reg prometheus.Registerer) *Metrics {
	m := &Metrics{
		eventsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "atproto_events_total",
			Help: "Total AT Protocol firehose events by kind, collection and operation.",
		}, []string{"kind", "collection", "operation"}),
		postsTotal: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "atproto_posts_total",
			Help: "Total posts observed by (allowlisted) language.",
		}, []string{"lang"}),
		firehoseLag: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "atproto_firehose_lag_seconds",
			Help: "Seconds between now and the time_us of the last processed Jetstream message.",
		}),
		eventsProcessed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "atproto_events_processed_total",
			Help: "Total Jetstream messages processed (throughput + restart detection).",
		}),
		plcOperations: prometheus.NewCounterVec(prometheus.CounterOpts{
			Name: "atproto_plc_operations_total",
			Help: "Total plc.directory operations observed by operation type.",
		}, []string{"operation"}),
		federationPDS: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "atproto_federation_pds_count",
			Help: "Distinct PDS endpoints seen in the PLC export log.",
		}),
		topDomains: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "atproto_top_domains",
			Help: "Occurrence count of the top-N linked domains over the rolling window.",
		}, []string{"domain"}),
		topHashtags: prometheus.NewGaugeVec(prometheus.GaugeOpts{
			Name: "atproto_top_hashtags",
			Help: "Occurrence count of the top-N hashtags over the rolling window.",
		}, []string{"hashtag"}),
		wsConnected: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "atproto_exporter_ws_connected",
			Help: "Whether the Jetstream WebSocket is currently connected (1) or not (0).",
		}),
		wsReconnects: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "atproto_exporter_ws_reconnects_total",
			Help: "Total Jetstream WebSocket reconnect attempts.",
		}),
		plcPollErrors: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "atproto_exporter_plc_poll_errors_total",
			Help: "Total plc.directory poll errors.",
		}),
		plcPollDuration: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "atproto_exporter_plc_poll_duration_seconds",
			Help:    "Duration of plc.directory poll requests.",
			Buckets: prometheus.DefBuckets,
		}),
	}
	m.TopDomains = &TopNGauge{vec: m.topDomains}
	m.TopHashtags = &TopNGauge{vec: m.topHashtags}

	reg.MustRegister(
		m.eventsTotal, m.postsTotal, m.firehoseLag, m.eventsProcessed,
		m.plcOperations, m.federationPDS, m.topDomains, m.topHashtags,
		m.wsConnected, m.wsReconnects, m.plcPollErrors, m.plcPollDuration,
	)
	return m
}

// --- Jetstream façade (callers pass normalized label values) ---

// ObserveEvent increments the event counter and the processed counter.
func (m *Metrics) ObserveEvent(kind, collection, operation string) {
	m.eventsTotal.WithLabelValues(kind, collection, operation).Inc()
	m.eventsProcessed.Inc()
}

// ObservePost increments the posts counter for a normalized language.
func (m *Metrics) ObservePost(lang string) {
	m.postsTotal.WithLabelValues(lang).Inc()
}

// SetFirehoseLag sets the firehose lag gauge in seconds.
func (m *Metrics) SetFirehoseLag(seconds float64) { m.firehoseLag.Set(seconds) }

// SetWSConnected sets the WebSocket connectivity gauge.
func (m *Metrics) SetWSConnected(up bool) {
	if up {
		m.wsConnected.Set(1)
	} else {
		m.wsConnected.Set(0)
	}
}

// IncWSReconnects increments the reconnect counter.
func (m *Metrics) IncWSReconnects() { m.wsReconnects.Inc() }

// --- PLC façade ---

// PLCOperation increments the PLC op counter for a normalized operation type.
func (m *Metrics) PLCOperation(operation string) {
	m.plcOperations.WithLabelValues(operation).Inc()
}

// SetFederationPDS sets the distinct-PDS gauge.
func (m *Metrics) SetFederationPDS(n int) { m.federationPDS.Set(float64(n)) }

// IncPLCPollErrors increments the PLC poll error counter.
func (m *Metrics) IncPLCPollErrors() { m.plcPollErrors.Inc() }

// ObservePLCPollDuration records a PLC poll duration in seconds.
func (m *Metrics) ObservePLCPollDuration(seconds float64) {
	m.plcPollDuration.Observe(seconds)
}

// --- Accessors (used for assertions in tests) ---

// EventsTotalVec returns the events counter vector.
func (m *Metrics) EventsTotalVec() *prometheus.CounterVec { return m.eventsTotal }

// PostsTotalVec returns the posts counter vector.
func (m *Metrics) PostsTotalVec() *prometheus.CounterVec { return m.postsTotal }

// PLCOperationsVec returns the PLC operations counter vector.
func (m *Metrics) PLCOperationsVec() *prometheus.CounterVec { return m.plcOperations }

// FirehoseLagCollector returns the firehose-lag gauge.
func (m *Metrics) FirehoseLagCollector() prometheus.Collector { return m.firehoseLag }

// EventsProcessedCollector returns the events-processed counter.
func (m *Metrics) EventsProcessedCollector() prometheus.Collector { return m.eventsProcessed }

// FederationPDSCollector returns the federation-PDS gauge.
func (m *Metrics) FederationPDSCollector() prometheus.Collector { return m.federationPDS }

// WSConnectedCollector returns the ws-connected gauge.
func (m *Metrics) WSConnectedCollector() prometheus.Collector { return m.wsConnected }

// WSReconnectsCollector returns the ws-reconnects counter.
func (m *Metrics) WSReconnectsCollector() prometheus.Collector { return m.wsReconnects }

// PLCPollErrorsCollector returns the PLC poll-errors counter.
func (m *Metrics) PLCPollErrorsCollector() prometheus.Collector { return m.plcPollErrors }

// TopNGauge applies a topn.Snapshot to a single-label gauge vector, setting the
// current top entries and deleting any series that fell out of the top set.
type TopNGauge struct {
	vec *prometheus.GaugeVec
}

// Sync updates the gauge vector to reflect s: it sets every current top entry
// and deletes every evicted key, keeping series count == len(s.Top) <= N.
func (g *TopNGauge) Sync(s topn.Snapshot) {
	for _, e := range s.Top {
		g.vec.WithLabelValues(e.Key).Set(float64(e.Count))
	}
	for _, k := range s.Removed {
		g.vec.DeleteLabelValues(k)
	}
}
