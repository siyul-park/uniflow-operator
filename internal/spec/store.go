package spec

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sync"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

// Store interacts with a remote specification store.
type Store struct {
	url string
}

// Stream manages a WebSocket connection for receiving events.
type Stream struct {
	conn *websocket.Conn
	next chan Event
	done chan struct{}
	mu   sync.Mutex
}

// Event describes a change operation in the store.
type Event struct {
	OP EventOP   // Type of the event
	ID uuid.UUID // ID of the affected specification
}

// EventOP represents possible types of events.
type EventOP string

const (
	EventStore  EventOP = "store"  // New specification stored
	EventSwap   EventOP = "swap"   // Specification updated
	EventDelete EventOP = "delete" // Specification deleted
)

// NewStore initializes a new Store instance.
func NewStore(url string) *Store {
	return &Store{url: url}
}

// Watch opens a WebSocket connection to monitor specification changes.
func (s *Store) Watch(ctx context.Context) (*Stream, error) {
	parse, err := url.Parse(s.url)
	if err != nil {
		return nil, err
	}

	parse.Path = "/v1/specs"
	parse.RawQuery = "watch=true"

	if parse.Scheme == "http" {
		parse.Scheme = "ws"
	}
	if parse.Scheme == "https" {
		parse.Scheme = "wss"
	}

	conn, err := websocket.Dial(parse.String(), "", s.url)
	if err != nil {
		return nil, fmt.Errorf("failed to establish WebSocket connection: %w", err)
	}

	stream := &Stream{
		conn: conn,
		next: make(chan Event),
		done: make(chan struct{}),
	}

	go stream.listen()
	go stream.close(ctx)

	return stream, nil
}

// Load retrieves specifications by various identifiers.
func (s *Store) Load(ctx context.Context, specs ...Spec) ([]Spec, error) {
	query := url.Values{}
	for _, spec := range specs {
		if id := spec.GetID(); id != uuid.Nil {
			query.Add("id", id.String())
		}
		if namespace := spec.GetNamespace(); namespace != "" {
			query.Add("namespace", namespace)
		}
		if name := spec.GetName(); name != "" {
			query.Add("name", name)
		}
	}

	resp, err := s.get(ctx, "/v1/specs", query)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status: %d", resp.StatusCode)
	}

	var unstructured []*Unstructured
	if err := json.NewDecoder(resp.Body).Decode(&unstructured); err != nil {
		return nil, fmt.Errorf("failed to decode response body: %w", err)
	}

	result := make([]Spec, len(specs))
	for i, spec := range unstructured {
		result[i] = spec
	}
	return result, nil
}

// get sends an HTTP GET request to the specified URL.
func (s *Store) get(ctx context.Context, path string, query url.Values) (*http.Response, error) {
	parse, err := url.ParseRequestURI(s.url)
	if err != nil {
		return nil, err
	}

	parse.Path = path
	parse.RawQuery = query.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, parse.String(), nil)
	if err != nil {
		return nil, err
	}
	return http.DefaultClient.Do(req)
}

// Stream methods

// Next provides a channel for receiving incoming events.
func (s *Stream) Next() <-chan Event {
	return s.next
}

// Done signals when the stream is closed.
func (s *Stream) Done() <-chan struct{} {
	return s.done
}

// Close shuts down the WebSocket connection and associated channels.
func (s *Stream) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	select {
	case <-s.done:
		return nil
	default:
		close(s.done)
		return s.conn.Close()
	}
}

// listen processes incoming WebSocket messages and sends events to the channel.
func (s *Stream) listen() {
	defer close(s.next)
	for {
		var event Event
		if err := websocket.JSON.Receive(s.conn, &event); err != nil {
			if errors.Is(err, io.EOF) {
				_ = s.Close()
			}
			break
		}
		s.next <- event
	}
}

// close ensures the stream is closed when the context is canceled.
func (s *Stream) close(ctx context.Context) {
	select {
	case <-s.Done():
	case <-ctx.Done():
		_ = s.Close()
	}
}
