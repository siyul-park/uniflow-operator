package spec

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Store", func() {
	specs := []Spec{
		&Unstructured{
			Meta: Meta{
				ID:        uuid.New(),
				Namespace: "test-ns",
				Name:      "test-name",
			},
		},
	}

	var server *httptest.Server

	BeforeEach(func() {
		server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			Expect(r.URL.Path).To(Equal("/v1/specs"))

			if r.URL.Query().Get("watch") == "true" {
				handler := func(conn *websocket.Conn) {
					defer func() { _ = conn.Close() }()

					for i := 0; i < 3; i++ {
						event := Event{
							OP: EventStore,
							ID: uuid.New(),
						}
						err := websocket.JSON.Send(conn, event)
						Expect(err).To(Succeed())
						time.Sleep(100 * time.Millisecond)
					}
				}
				websocket.Handler(handler).ServeHTTP(w, r)
			} else {
				Expect(json.NewEncoder(w).Encode(specs)).To(Succeed())
			}
		}))
	})

	AfterEach(func() {
		server.Close()
	})

	It("should load specs successfully", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		store := NewStore(server.URL)

		actual, err := store.Load(ctx, specs...)
		Expect(err).NotTo(HaveOccurred())
		Expect(actual).To(HaveLen(len(specs)))
	})

	It("should watch specs successfully", func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		store := NewStore(server.URL)

		stream, err := store.Watch(ctx)
		Expect(err).NotTo(HaveOccurred())
		Expect(stream).NotTo(BeNil())

		select {
		case event, ok := <-stream.Next():
			Expect(ok).To(BeTrue())
			Expect(event).NotTo(BeZero())
		case <-ctx.Done():
			Expect(ctx.Err()).NotTo(HaveOccurred())
		}
	})
})
