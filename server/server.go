package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/sync/errgroup"
	"golang.org/x/time/rate"

	websocket "nhooyr.io/websocket"
	wsjson "nhooyr.io/websocket/wsjson"

	hex "github.com/mikeyg42/HexGame/structures"
	evt "github.com/mikeyg42/HexGame/structures/lobby"
)

const topicCodeLength = 5                  // fixed legth of the topic code
const heartbeatInterval = time.Second * 30 // 30 seconds, adjust as needed

// move this to the main MAIN funtion
func main() {

	lobbyServ := newChatServer()
	err := runWebsocketServer(lobbyServ, "localhost:8080")
	if err != nil {
		panic(err)
	}

	go func() {
		for {
			time.Sleep(heartbeatInterval)
			lobbyServ.UnresponsiveClients()
		}
	}()

}

// run initializes the lobbyServer and starts the HTTP server.
func runWebsocketServer(lb *lobbyServer, tcpAddr string) error {
	mainCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	listener, err := net.Listen("tcp", tcpAddr)
	if err != nil {
		return err
	}
	log.Printf("listening on http://%v", listener.Addr())

	// Create a new HTTP server and set the base context to our main context.
	server := &http.Server{
		Handler: newChatServer(),
		BaseContext: func(_ net.Listener) context.Context {
			return mainCtx
		},
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
		// set the max read and write too??
	}
	s := &myServer{server: server}

	// make error channel to consolidate for errors from the server
	egroup, eGroupCtx := errgroup.WithContext(s.server.BaseContext(listener))

	// Goroutine for serving HTTP
	egroup.Go(func() error {
		return server.Serve(listener)
	})

	// Goroutine to handle shutdown signals
	egroup.Go(func() error {
		// Wait for all goroutines in the errgroup
		if err := egroup.Wait(); err != nil && err != http.ErrServerClosed {
			defer s.server.Shutdown(eGroupCtx)

			log.Printf("failed to serve: %v", err)

			return err
		}
		return nil
	})

	s.server.RegisterOnShutdown()

	return nil
}

// lobbyServer enables broadcasting to a set of subscriberserver.
type lobbyServer struct {
	// max number of eventMsgs that can be queued for a subscriber before it is lost. Defaults to 16.
	subscriberEventMsgBuffer int

	// publishLimiter controls the rate limit applied to the publish endpoint.
	// Defaults to one publish every 100ms with a burst of 8.
	publishLimiter *rate.Limiter

	// where logs are sent.
	logf func(f string, v ...interface{})

	// serveMux routes the various endpoints to the appropriate handler.
	serveMux http.ServeMux

	subscribersMu sync.Mutex
	subscribers   map[*subscriber]struct{} // struct contains the events channel and a func to call if they "cant hang"

	lastHeartbeat time.Time // Add this field to track the last heartbeat
}

type myServer struct {
	server *http.Server
}

func (s *myServer) ShutdownServer(eGroupCtx, mainCtx context.Context) error {
	for {
		select {
		case <-eGroupCtx.Done():
			return eGroupCtx.Err()

		case <-mainCtx.Done():
			shutdownCtx, cancel := context.WithTimeout(mainCtx, time.Second*10)
			defer cancel()

			// Trigger graceful server shutdown - give it 10 seconds to try to finish
			s.server.Shutdown(shutdownCtx)

			log.Println("server gracefully shut down")
			return nil
		}
	}
}

// newChatServer constructs a lobbyServer with the defaultserver.
func newChatServer() *lobbyServer {
	lobbyServ := &lobbyServer{
		subscriberEventMsgBuffer: 16,
		logf:                     log.Printf,
		subscribers:              make(map[*subscriber]struct{}),
		publishLimiter:           rate.NewLimiter(rate.Every(time.Millisecond*100), 8),
	}
	lobbyServ.serveMux.Handle("/", http.FileServer(http.Dir(".")))
	lobbyServ.serveMux.HandleFunc("/subscribe", lobbyServ.subscribeHandler)
	lobbyServ.serveMux.HandleFunc("/publish", lobbyServ.publishHandler)

	return lobbyServ
}

// EventMsgs are sent on the EventMessage channel and .
type subscriber struct {
	evts          chan []byte // channel to receive dispatches from eventBus
	closeTooSlow  func()      // if the client cannot keep up with the eventMsgs, closeTooSlow is called
	lastHeartbeat time.Time   // will allow us to ascertain if the client is still connected/responsive
}

func (lobbyServ *lobbyServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	lobbyServ.serveMux.ServeHTTP(w, r)
}

// subscribeHandler accepts the WebSocket connection and then subscribes
// it to all future event messages. (Newly Refactored Version)
func (lobbyServ *lobbyServer) subscribeHandler(w http.ResponseWriter, r *http.Request) {
	// Accept WebSocket connection (same as in your existing code)
	c, err := websocket.Accept(w, r, nil)
	if err != nil {
		lobbyServ.logf("%v", err)
		return
	}
	defer c.Close(websocket.StatusInternalError, "")

	// Existing subscription logic...
	err = lobbyServ.subscribe(r.Context(), c)
	if handleConnectionClose(err) {
		return
	}

	// Initialize the last heartbeat time
	lobbyServ.lastHeartbeat = time.Now()

	// Set up error group for managing concurrent operations (new)
	g, ctx := errgroup.WithContext(r.Context())

	// Handle WebSocket events (new)
	g.Go(func() error {
		return lobbyServ.handleWebSocketEvents(ctx, c)
	})

	// Handle heartbeat messages (new)
	g.Go(func() error {
		return lobbyServ.sendHeartbeatMessages(ctx, c)
	})

	// Wait for all goroutines to finish (new)
	if err := g.Wait(); err != nil {
		lobbyServ.logf("Error: %v", err)
	}
}

func (lobbyServ *lobbyServer) checkUnresponsiveClients() {
	lobbyServ.subscribersMu.Lock()
	defer lobbyServ.subscribersMu.Unlock()

	for s := range lobbyServ.subscribers {
		if time.Since(s.lastHeartbeat) > 2*heartbeatInterval {
			go s.closeTooSlow() // or any other action you deem appropriate
		}
	}
}

// publishHandler reads the request body with a limit of 8192 bytes and then publishes
// the received eventMsg.
func (lobbyServ *lobbyServer) publishHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
		return
	}
	body := http.MaxBytesReader(w, r.Body, 8192)
	evt, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, http.StatusText(http.StatusRequestEntityTooLarge), http.StatusRequestEntityTooLarge)
		return
	}

	lobbyServ.publish(evt)

	w.WriteHeader(http.StatusAccepted)
}

// creates a subscriber
func (lobbyServ *lobbyServer) subscribe(ctx context.Context, c *websocket.Conn) error {
	ctx = c.CloseRead(ctx)

	s := &subscriber{
		evts: make(chan []byte, lobbyServ.subscriberEventMsgBuffer),
		closeTooSlow: func() {
			c.Close(websocket.StatusPolicyViolation, "connection too slow to keep up with eventMsgs")
		},
	}

	lobbyServ.AddSubscriber(s)
	defer lobbyServ.DeleteSubscriber(s)

	for {
		select {
		case evt := <-s.evts:
			if err := writeTimeout(ctx, time.Second*5, c, evt); err != nil {
				return err
			}
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

// publish publishes an evt to all subscribers. It never blocks and so eventMsgs to slow subscribers are dropped.
func (lobbyServ *lobbyServer) publish(evt []byte) {
	lobbyServ.subscribersMu.Lock()
	defer lobbyServ.subscribersMu.Unlock()

	lobbyServ.publishLimiter.Wait(context.Background())

	for s := range lobbyServ.subscribers {
		select {
		case s.evts <- evt:
		default:
			go s.closeTooSlow()
		}
	}
}

// AddSubscriber registers a subscriber into the map.
func (lobbyServ *lobbyServer) AddSubscriber(s *subscriber) {
	lobbyServ.subscribersMu.Lock()
	lobbyServ.subscribers[s] = struct{}{}
	lobbyServ.subscribersMu.Unlock()
}

// removes a subscriber from the map of subscribers
func (lobbyServ *lobbyServer) DeleteSubscriber(s *subscriber) {
	lobbyServ.subscribersMu.Lock()
	delete(lobbyServ.subscribers, s)
	lobbyServ.subscribersMu.Unlock()
}

// writeTimeout writes a message to the WebSocket connection with a timeout
func writeTimeout(ctx context.Context, timeout time.Duration, c *websocket.Conn, msg []byte) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	return c.Write(ctx, websocket.MessageType(websocket.StatusInternalError), msg)

}

// sendHeartbeatMessages sends heartbeat messages at regular intervals
func (lobbyServ *lobbyServer) sendHeartbeatMessages(ctx context.Context, c *websocket.Conn) error {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			heartbeatMsg := []byte("heartbeat")
			if err := writeTimeout(ctx, time.Second*5, c, heartbeatMsg); err != nil {
				lobbyServ.logf("Heartbeat failed: %v", err)
				return err
			}
			lobbyServ.lastHeartbeat = time.Now()
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

func ReadFromWebsocket(ctx context.Context, c *websocket.Conn) (string, []byte, error) {

	msg := make([]byte, 0, 1024)
	err := wsjson.Read(ctx, c, &msg)
	if err != nil {
		return "", nil, err
	}

	// Ensure you've read enough bytes for the topic.
	if len(msg) < topicCodeLength+2 {
		// this +2 is not arbitrary. there will be  # delimiting topic and msg. and msg must be at least 1 char.
		return "", nil, errors.New("message too short to contain topic... abort before reading payload")
	}

	// Extract topic and return
	topic := string(msg[:topicCodeLength])
	return topic, msg[topicCodeLength:], nil
}

func WriteToWebsocket(ctx context.Context, c *websocket.Conn, msg []byte) error {
	yn_json := json.Valid([]byte(msg)) && len(msg) < 2
	if yn_json {
		err := wsjson.Write(ctx, c, msg)
		return err
	} else {
		return errors.New("message is not valid json")
	}
}

func handleWebsocketConnection(ctx context.Context, c *websocket.Conn, geb *evt.GameEventBus) {

	for {
		var msg []byte
		_, msg, err := ReadFromWebsocket(ctx, c)
		if err != nil {
			// Handle read error.
			return
		}

		// Here, you can prepend the topic.
		topic := "YOUR_TOPIC"                             // You need to determine how to set the topic.
		msgWithTopic := prependTopicToPayload(topic, msg) // prepends with the correct delimiter

		geb.DispatchMessage(String(msgWithTopic))
	}
}

func prependTopicToPayload(topic string, payload []byte) []byte {
	topicBytes := []byte(topic + hex.Delimiter)
	return append(topicBytes, payload...)
}

func (lobbyServ *lobbyServer) handleWebSocketEvents(ctx context.Context, c *websocket.Conn) error {
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			messageType, p, err := c.ReadMessage()
			if err != nil {
				if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
					lobbyServ.logf("WebSocket closed: %v", err)
					return nil
				}
				lobbyServ.logf("Error reading WebSocket message: %v", err)
				return err
			}

			if err := lobbyServ.processMessage(messageType, p); err != nil {
				lobbyServ.logf("Error processing message: %v", err)
				// Optionally send error response to client
				return err
			}
		}
	}
}

// processMessage handles different types of messages received over WebSocket
func (lobbyServ *lobbyServer) processMessage(messageType int, message []byte) error {
	// Parse and handle different message types
	// Example: MessageTypeMove, MessageTypeConnectivityCheck, MessageTypeShutdown
	switch messageType {
	case MessageTypeMove:
		return lobbyServ.handleMove(message)
	case MessageTypeConnectivityCheck:
		return lobbyServ.handleConnectivityCheck()
	case MessageTypeShutdown:

		return lobbyServ.handleShutdown()
	default:
		return fmt.Errorf("unknown message type: %d", messageType)
	}
}

// handleConnectivityCheck handles a connectivity check message
func (lobbyServ *lobbyServer) handleConnectivityCheck() error {
	// Perform necessary actions to check and confirm connectivity
}

// handleShutdown handles a shutdown message, indicating a game or server shutdown
func (lobbyServ *lobbyServer) handleShutdown() error {
	// Perform necessary actions for a graceful shutdown
}
