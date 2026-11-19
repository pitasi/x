package antopt

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const listenBrainzPlayingNowURL = "https://api.listenbrainz.org/1/user/zaphodias/playing-now"

type nowPlayingTrack struct {
	Name   string
	Artist string
}

func listenBrainzNowPlaying(ctx context.Context, endpoint string) (*nowPlayingTrack, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "anto.pt now-playing (https://anto.pt)")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", res.StatusCode)
	}

	var body struct {
		Payload struct {
			Listens []struct {
				Track struct {
					Name   string `json:"track_name"`
					Artist string `json:"artist_name"`
				} `json:"track_metadata"`
			} `json:"listens"`
		} `json:"payload"`
	}
	if err := json.NewDecoder(io.LimitReader(res.Body, 256<<10)).Decode(&body); err != nil {
		return nil, err
	}
	if len(body.Payload.Listens) == 0 || body.Payload.Listens[0].Track.Name == "" {
		return nil, nil
	}
	track := body.Payload.Listens[0].Track
	return &nowPlayingTrack{Name: track.Name, Artist: track.Artist}, nil
}

type nowPlayingSubs struct {
	mu   *sync.Mutex
	subs map[chan []byte]struct{}
	last []byte
}

func (s *nowPlayingSubs) subscribe(c chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.subs[c] = struct{}{}
	c <- s.last
}

func (s *nowPlayingSubs) unsubscribe(c chan []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.subs, c)
}

func (s *nowPlayingSubs) broadcast(next []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.last = next
	for c := range s.subs {
		// Each subscriber only needs the latest state, even if it missed an update.
		select {
		case <-c:
		default:
		}
		c <- next
	}
}

func (ws Website) nowPlaying(mux *http.ServeMux) {
	subs := &nowPlayingSubs{
		mu:   &sync.Mutex{},
		subs: make(map[chan []byte]struct{}),
		last: []byte("null"),
	}

	// Start one shared poller when the first browser opens the stream.
	var start sync.Once
	keepalive := 30 * time.Second
	mux.HandleFunc("GET /now-playing", func(w http.ResponseWriter, r *http.Request) {
		start.Do(func() { go subs.poll() })
		ctrl := http.NewResponseController(w)

		w.Header().Set("content-type", "text/event-stream")
		w.Header().Set("cache-control", "no-cache")
		w.WriteHeader(200)

		newTrack := make(chan []byte, 1)
		subs.subscribe(newTrack)
		defer func() {
			subs.unsubscribe(newTrack)
			close(newTrack)
		}()

		keepaliveTimer := time.NewTicker(keepalive)
		defer keepaliveTimer.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case <-keepaliveTimer.C:
				_, _ = w.Write([]byte("data: ping"))
				_, _ = w.Write([]byte("\n\n"))
				_ = ctrl.Flush()
			case data := <-newTrack:
				_, _ = w.Write([]byte("event: now-playing\n"))
				_, _ = w.Write([]byte("data: "))
				_, _ = w.Write(data)
				_, _ = w.Write([]byte("\n\n"))
				_ = ctrl.Flush()
			}
		}
	})
}

func (s *nowPlayingSubs) poll() {
	var last *nowPlayingTrack
	for {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		track, err := listenBrainzNowPlaying(ctx, listenBrainzPlayingNowURL)
		cancel()
		if err != nil {
			logger.Error("fetching ListenBrainz now playing", "err", err)
		}
		if (last == nil) != (track == nil) || (last != nil && track != nil && *last != *track) {
			data, err := json.Marshal(track)
			if err != nil {
				logger.Error("marshalling now playing track", "err", err)
			} else {
				last = track
				s.broadcast(data)
			}
		}
		time.Sleep(time.Minute)
	}
}
