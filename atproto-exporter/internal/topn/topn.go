// Package topn maintains an approximate top-N ranking of keys (e.g. linked
// domains or hashtags) observed over a rolling time window. It is the reusable
// engine behind the bounded top-N gauges: counts live in-memory only, and the
// exported set never exceeds N, so Prometheus cardinality stays bounded even
// though the key space (user-controlled) is not.
//
// The window is divided into fixed-duration buckets held in a ring; buckets
// older than the window are transparently dropped as time advances. Selection
// of the top N uses a bounded min-heap so at most N entries are ever retained
// during a scan.
package topn

import (
	"container/heap"
	"sort"
	"sync"
	"time"
)

const defaultBuckets = 60

// Entry is a key and its aggregated count over the current window.
type Entry struct {
	Key   string
	Count int64
}

// Snapshot is the result of Snapshot: the current top-N plus the keys that were
// exported in the previous snapshot but have since fallen out, so the caller can
// delete exactly those series once.
type Snapshot struct {
	Top     []Entry
	Removed []string
}

type bucket struct {
	epoch  int64 // which time bucket this slot currently represents
	counts map[string]int64
}

// Window is a concurrency-safe rolling-window top-N counter.
type Window struct {
	n          int
	bucketDur  time.Duration
	numBuckets int

	mu       sync.Mutex
	buckets  []bucket
	exported map[string]struct{}
}

// New returns a Window keeping the top n keys over the given rolling window.
func New(n int, window time.Duration) *Window {
	numBuckets := defaultBuckets
	bucketDur := window / time.Duration(numBuckets)
	if bucketDur <= 0 {
		bucketDur = 1
		numBuckets = int(window / bucketDur)
		if numBuckets < 1 {
			numBuckets = 1
		}
	}
	buckets := make([]bucket, numBuckets)
	for i := range buckets {
		buckets[i] = bucket{epoch: -1, counts: map[string]int64{}}
	}
	return &Window{
		n:          n,
		bucketDur:  bucketDur,
		numBuckets: numBuckets,
		buckets:    buckets,
		exported:   map[string]struct{}{},
	}
}

func (w *Window) epochOf(t time.Time) int64 {
	return t.UnixNano() / int64(w.bucketDur)
}

// Add records one observation of key at time t. Events are expected roughly in
// time order (the Jetstream firehose is ordered by time_us); an observation for
// a bucket that has already rolled forward in its ring slot is dropped, which is
// acceptable for an explicitly approximate top-N.
func (w *Window) Add(key string, t time.Time) {
	epoch := w.epochOf(t)
	slot := int(((epoch % int64(w.numBuckets)) + int64(w.numBuckets)) % int64(w.numBuckets))

	w.mu.Lock()
	defer w.mu.Unlock()

	b := &w.buckets[slot]
	switch {
	case b.epoch == epoch:
		b.counts[key]++
	case epoch > b.epoch:
		// Roll this slot forward to a new bucket.
		b.epoch = epoch
		b.counts = map[string]int64{key: 1}
	default:
		// epoch < b.epoch: stale observation for a slot already advanced; drop.
	}
}

// aggregate sums counts across all buckets still inside the window at time t.
// Caller must hold w.mu.
func (w *Window) aggregate(t time.Time) map[string]int64 {
	cur := w.epochOf(t)
	minEpoch := cur - int64(w.numBuckets) + 1
	totals := map[string]int64{}
	for i := range w.buckets {
		b := &w.buckets[i]
		if b.epoch < minEpoch || b.epoch > cur {
			continue
		}
		for k, c := range b.counts {
			totals[k] += c
		}
	}
	return totals
}

// Top returns the current top-N entries at time t, ordered by count descending
// then key ascending. It does not mutate the exported set.
func (w *Window) Top(t time.Time) []Entry {
	w.mu.Lock()
	totals := w.aggregate(t)
	w.mu.Unlock()
	return selectTopN(totals, w.n)
}

// Snapshot returns the current top-N and the keys removed from the exported set
// since the previous Snapshot. Each evicted key is reported exactly once.
func (w *Window) Snapshot(t time.Time) Snapshot {
	w.mu.Lock()
	totals := w.aggregate(t)
	top := selectTopN(totals, w.n)

	next := make(map[string]struct{}, len(top))
	for _, e := range top {
		next[e.Key] = struct{}{}
	}
	var removed []string
	for k := range w.exported {
		if _, ok := next[k]; !ok {
			removed = append(removed, k)
		}
	}
	w.exported = next
	w.mu.Unlock()

	return Snapshot{Top: top, Removed: removed}
}

// greater reports whether entry a should rank above b (count desc, key asc).
func greater(a, b Entry) bool {
	if a.Count != b.Count {
		return a.Count > b.Count
	}
	return a.Key < b.Key
}

// minHeap keeps the n best entries; its root is the weakest retained entry.
type minHeap []Entry

func (h minHeap) Len() int           { return len(h) }
func (h minHeap) Less(i, j int) bool { return greater(h[j], h[i]) } // root = weakest
func (h minHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *minHeap) Push(x any)        { *h = append(*h, x.(Entry)) }
func (h *minHeap) Pop() any {
	old := *h
	n := len(old)
	e := old[n-1]
	*h = old[:n-1]
	return e
}

func selectTopN(totals map[string]int64, n int) []Entry {
	if n <= 0 || len(totals) == 0 {
		return nil
	}
	h := &minHeap{}
	heap.Init(h)
	for k, c := range totals {
		e := Entry{Key: k, Count: c}
		if h.Len() < n {
			heap.Push(h, e)
			continue
		}
		if greater(e, (*h)[0]) {
			heap.Pop(h)
			heap.Push(h, e)
		}
	}
	out := make([]Entry, h.Len())
	for i := len(out) - 1; i >= 0; i-- {
		out[i] = heap.Pop(h).(Entry)
	}
	sort.SliceStable(out, func(i, j int) bool { return greater(out[i], out[j]) })
	return out
}
