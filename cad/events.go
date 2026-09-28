package main

import (
	"encoding/json"
	"log"
	"sort"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Collectors register themselves from init() in their own file; server.go never lists them.
type collector struct {
	topic   string
	every   func() time.Duration // read each scheduling cycle, so it can follow the policy
	collect func() (interface{}, error)
}

var collectors []collector

// pollEvery is the cadence of the cheap collectors (policy, capacity, workers).
const pollEvery = 2 * time.Second

func register(topic string, every time.Duration, f func() (interface{}, error)) {
	collectors = append(collectors, collector{topic, fixed(every), f})
}

func fixed(d time.Duration) func() time.Duration { return func() time.Duration { return d } }

type Event struct {
	Topic string          `json:"topic"`
	Rev   uint64          `json:"rev"`
	At    time.Time       `json:"at"`
	Data  json.RawMessage `json:"data"`
}

type Quota struct {
	Reviewer  string     `json:"reviewer"`
	State     string     `json:"state"`
	LastHitAt *time.Time `json:"lastHitAt,omitempty"`
	ResetAt   *time.Time `json:"resetAt,omitempty"`
	Source    string     `json:"source"`
}

const ringSize = 256

type hub struct {
	mu   sync.Mutex
	rev  uint64
	cur  map[string]Event
	keys map[string]string // comparison key per topic (JSON without collectedAt)
	ring []Event
	subs map[chan Event]struct{}

	qmu    sync.Mutex
	quotas map[string]Quota
}

func newHub() *hub {
	h := &hub{cur: map[string]Event{}, keys: map[string]string{}, subs: map[chan Event]struct{}{}, quotas: map[string]Quota{}}
	h.rev = uint64(time.Now().UnixMilli()) // revs stay increasing across restarts, so stale Last-Event-IDs never collide
	h.publish("workers", []struct{}{})
	h.publish("quota", []Quota{})
	return h
}

func compareKey(b []byte) string {
	var m map[string]interface{}
	if json.Unmarshal(b, &m) == nil {
		delete(m, "collectedAt")
		b, _ = json.Marshal(m)
	}
	return string(b)
}

// publish records v as topic's value and fans it out, only if it changed.
func (h *hub) publish(topic string, v interface{}) {
	data, err := json.Marshal(v)
	if err != nil {
		log.Printf("cad: marshal %s: %v", topic, err)
		return
	}
	key := compareKey(data)
	if k, ok := v.(interface{ changeKey() interface{} }); ok {
		b, _ := json.Marshal(k.changeKey())
		key = string(b)
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if old, ok := h.keys[topic]; ok && old == key {
		e := h.cur[topic] // keep GET fresh; only the SSE fan-out is gated by the key
		e.At, e.Data = time.Now().UTC(), data
		h.cur[topic] = e
		return
	}
	h.rev++
	e := Event{Topic: topic, Rev: h.rev, At: time.Now().UTC(), Data: data}
	h.cur[topic], h.keys[topic] = e, key
	h.ring = append(h.ring, e)
	if len(h.ring) > ringSize {
		h.ring = h.ring[1:]
	}
	for c := range h.subs {
		select {
		case c <- e:
		default: // ponytail: slow client is dropped; it reconnects with Last-Event-ID
			close(c)
			delete(h.subs, c)
		}
	}
}

// snapshot returns current events in rev order (so SSE ids never go backwards). Caller holds h.mu.
func (h *hub) snapshot() []Event {
	out := make([]Event, 0, len(h.cur))
	for _, e := range h.cur {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Rev < out[j].Rev })
	return out
}

func (h *hub) current() []Event {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.snapshot()
}

// subscribe registers a channel and returns what to send first: a ring replay
// after lastID when the ring still covers it, otherwise the full snapshot.
func (h *hub) subscribe(lastID string) (chan Event, []Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	ch := make(chan Event, 64)
	h.subs[ch] = struct{}{}
	if id, err := strconv.ParseUint(lastID, 10, 64); err == nil && id <= h.rev && len(h.ring) > 0 && h.ring[0].Rev <= id+1 {
		var out []Event
		for _, e := range h.ring {
			if e.Rev > id {
				out = append(out, e)
			}
		}
		return ch, out
	}
	return ch, h.snapshot()
}

func (h *hub) unsubscribe(ch chan Event) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.subs[ch]; ok {
		delete(h.subs, ch)
		close(ch)
	}
}

func (h *hub) setQuota(q Quota) {
	h.qmu.Lock()
	defer h.qmu.Unlock()
	if q.State == "exhausted" {
		now := time.Now().UTC()
		q.LastHitAt = &now
	} else if old, ok := h.quotas[q.Reviewer]; ok {
		q.LastHitAt = old.LastHitAt
	}
	h.quotas[q.Reviewer] = q
	list := make([]Quota, 0, len(h.quotas))
	for _, v := range h.quotas {
		list = append(list, v)
	}
	sort.Slice(list, func(i, j int) bool { return list[i].Reviewer < list[j].Reviewer })
	h.publish("quota", list)
}

func (h *hub) collectOne(c collector) {
	v, err := c.collect()
	if err != nil {
		log.Printf("cad: collect %s: %v", c.topic, err)
		return
	}
	h.publish(c.topic, v)
}

// run starts every collector now and again once its interval has passed since its last run, checking
// every tick with its current interval (so a shortened one applies at once) until stop closes. Each
// run gets its own goroutine and a collector still in flight is skipped, so a slow collector never
// delays the others (it runs again on the first tick after it finishes and is due).
func (h *hub) run(cs []collector, tick time.Duration, stop <-chan struct{}) {
	last := make([]time.Time, len(cs))
	busy := make([]atomic.Bool, len(cs))
	t := time.NewTicker(tick)
	defer t.Stop()
	for {
		now := time.Now()
		for i := range cs {
			if now.Before(last[i].Add(cs[i].every())) || !busy[i].CompareAndSwap(false, true) {
				continue
			}
			last[i] = now
			go func() {
				defer busy[i].Store(false)
				h.collectOne(cs[i])
			}()
		}
		select {
		case <-t.C:
		case <-stop:
			return
		}
	}
}

// topic decodes the published value of name into v; false if it is not published (yet).
func (h *hub) topic(name string, v interface{}) bool {
	for _, e := range h.current() {
		if e.Topic == name {
			return json.Unmarshal(e.Data, v) == nil
		}
	}
	return false
}
