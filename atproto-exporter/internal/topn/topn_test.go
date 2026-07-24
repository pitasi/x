package topn

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

var base = time.Unix(1_700_000_000, 0)

func keysOf(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Key
	}
	return out
}

func TestTopOrdersByCountDesc(t *testing.T) {
	w := New(3, time.Hour)
	add := func(k string, n int) {
		for i := 0; i < n; i++ {
			w.Add(k, base)
		}
	}
	add("a", 5)
	add("b", 10)
	add("c", 1)
	add("d", 7)

	top := w.Top(base)
	if got, want := keysOf(top), []string{"b", "d", "a"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Top = %v, want %v", got, want)
	}
	if top[0].Count != 10 {
		t.Errorf("top[0].Count = %d, want 10", top[0].Count)
	}
}

func TestTopTieBreakByKey(t *testing.T) {
	w := New(2, time.Hour)
	for _, k := range []string{"zebra", "apple", "mango"} {
		w.Add(k, base) // all count 1
	}
	// All tie at 1; deterministic order is key ascending.
	top := w.Top(base)
	if got, want := keysOf(top), []string{"apple", "mango"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Top tie-break = %v, want %v", got, want)
	}
}

func TestRollingWindowExpiry(t *testing.T) {
	w := New(5, time.Hour)
	w.Add("old", base)

	// Still inside the window a bit later.
	if got := len(w.Top(base.Add(time.Minute))); got != 1 {
		t.Fatalf("within window: len = %d, want 1", got)
	}
	// Past the window: the entry has aged out.
	if got := len(w.Top(base.Add(time.Hour + 2*time.Minute))); got != 0 {
		t.Fatalf("past window: len = %d, want 0 (expired)", got)
	}
}

func TestRollingWindowPartialExpiry(t *testing.T) {
	w := New(5, time.Hour)
	w.Add("early", base)
	w.Add("late", base.Add(30*time.Minute))

	// 40min after base: "early" (t=0) still in the 1h window, "late" too.
	got := len(w.Top(base.Add(40 * time.Minute)))
	if got != 2 {
		t.Fatalf("at +40m: len = %d, want 2", got)
	}
	// 70min after base: "early" (age 70m) expired, "late" (age 40m) remains.
	top := w.Top(base.Add(70 * time.Minute))
	if got, want := keysOf(top), []string{"late"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("at +70m: %v, want %v", got, want)
	}
}

func TestSnapshotEvictsExactlyOnce(t *testing.T) {
	w := New(2, time.Hour)
	w.Add("a", base)
	w.Add("a", base)
	w.Add("b", base)

	// First snapshot: a, b in; nothing removed yet.
	s1 := w.Snapshot(base)
	if got, want := keysOf(s1.Top), []string{"a", "b"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("s1.Top = %v, want %v", got, want)
	}
	if len(s1.Removed) != 0 {
		t.Fatalf("s1.Removed = %v, want none", s1.Removed)
	}

	// Push c and d past b so b falls out of the top-2.
	w.Add("c", base)
	w.Add("c", base)
	w.Add("c", base)
	w.Add("d", base)
	w.Add("d", base)
	w.Add("d", base)

	s2 := w.Snapshot(base)
	if got, want := keysOf(s2.Top), []string{"c", "d"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("s2.Top = %v, want %v", got, want)
	}
	// a and b left the exported set: reported removed exactly once.
	sort.Strings(s2.Removed)
	if want := []string{"a", "b"}; !reflect.DeepEqual(s2.Removed, want) {
		t.Fatalf("s2.Removed = %v, want %v", s2.Removed, want)
	}

	// Third snapshot with no change: nothing removed again (exactly once).
	s3 := w.Snapshot(base)
	if len(s3.Removed) != 0 {
		t.Fatalf("s3.Removed = %v, want none (already evicted)", s3.Removed)
	}
}

func TestEmptyTop(t *testing.T) {
	w := New(10, time.Hour)
	if got := w.Top(base); len(got) != 0 {
		t.Fatalf("empty Top = %v, want []", got)
	}
	s := w.Snapshot(base)
	if len(s.Top) != 0 || len(s.Removed) != 0 {
		t.Fatalf("empty Snapshot = %+v", s)
	}
}

func TestConcurrentAdd(t *testing.T) {
	w := New(5, time.Hour)
	done := make(chan struct{})
	for g := 0; g < 4; g++ {
		go func() {
			for i := 0; i < 1000; i++ {
				w.Add("k", base)
			}
			done <- struct{}{}
		}()
	}
	for g := 0; g < 4; g++ {
		<-done
	}
	top := w.Top(base)
	if len(top) != 1 || top[0].Count != 4000 {
		t.Fatalf("concurrent Add = %+v, want k=4000", top)
	}
}
