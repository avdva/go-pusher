package pusher

import (
	"encoding/json"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/pkg/errors"
	"golang.org/x/net/websocket"
)

// Client is a pusher client.
type Client struct {
	ws                 *websocket.Conn
	subscribedChannels *subscribedChannels
	binders            map[string]chan *Event
	m                  sync.RWMutex
	heartbeatStopCh    chan struct{}
}

// heartbeat send a ping frame to server each - TODO reconnect on disconnect
func (c *Client) heartbeat() {
	for {
		websocket.Message.Send(c.ws, `{"event":"pusher:ping","data":"{}"}`)
		select {
		case <-time.After(HEARTBEAT_RATE * time.Second):
		case <-c.heartbeatStopCh:
			return
		}
	}
}

// listen to Pusher server and process/dispatch recieved events
func (c *Client) listen() {
	for {
		var event Event
		err := websocket.JSON.Receive(c.ws, &event)
		if err != nil {
			c.Close()
			return
		}
		switch event.Event {
		case "pusher:ping":
			websocket.Message.Send(c.ws, `{"event":"pusher:pong","data":"{}"}`)
		case "pusher:pong":
		case "pusher:error":
			log.Println("Event error recieved: ", event.Data)
		default:
			c.m.RLock()
			binder, ok := c.binders[event.Event]
			if ok {
				binder <- &event
			}
			c.m.RUnlock()
		}
	}
}

// Subscribe to a channel
func (c *Client) Subscribe(channel string) (err error) {
	// Already subscribed ?
	if c.subscribedChannels.contains(channel) {
		err = errors.Errorf("Channel %s already subscribed", channel)
		return
	}
	err = websocket.Message.Send(c.ws, fmt.Sprintf(`{"event":"pusher:subscribe","data":{"channel":"%s"}}`, channel))
	if err != nil {
		return
	}
	err = c.subscribedChannels.add(channel)
	return
}

// Unsubscribe from a channel
func (c *Client) Unsubscribe(channel string) (err error) {
	// subscribed ?
	if !c.subscribedChannels.contains(channel) {
		err = errors.Errorf("Client isn't subscrived to %s", channel)
		return
	}
	err = websocket.Message.Send(c.ws, fmt.Sprintf(`{"event":"pusher:unsubscribe","data":{"channel":"%s"}}`, channel))
	if err != nil {
		return
	}
	// Remove channel from subscribedChannels slice
	c.subscribedChannels.remove(channel)
	return
}

// Bind an event. Bind returns a channel, where events will be sent.
// The channel will be closed, if a websocket read error occurs.
func (c *Client) Bind(evt string) (dataChannel chan *Event, err error) {
	// Already binded
	c.m.Lock()
	defer c.m.Unlock()
	_, ok := c.binders[evt]
	if ok {
		err = errors.Errorf("Event %s already binded", evt)
		return
	}
	// New data channel
	dataChannel = make(chan *Event, EVENT_CHANNEL_BUFF_SIZE)
	c.binders[evt] = dataChannel
	return
}

// Unbind a event
func (c *Client) Unbind(evt string) {
	c.m.Lock()
	delete(c.binders, evt)
	c.m.Unlock()
}

// Close closes websocket conn and unbinds all events.
func (c *Client) Close() {
	c.ws.Close()
	c.m.Lock()
	for evt, binder := range c.binders {
		close(binder)
		delete(c.binders, evt)
	}
	select {
	case c.heartbeatStopCh <- struct{}{}:
	default:
	}
	c.m.Unlock()
}

// NewCustomClient initialize & return a Pusher client for given host and sheme.
func NewCustomClient(appKey, host, scheme string) (*Client, error) {
	origin := "http://localhost/"
	url := scheme + "://" + host + "/app/" + appKey + "?protocol=" + PROTOCOL_VERSION
	ws, err := websocket.Dial(url, "", origin)
	if err != nil {
		return nil, err
	}
	var resp = make([]byte, 11000) // Pusher max message size is 10KB
	n, err := ws.Read(resp)
	if err != nil {
		return nil, err
	}
	var event Event
	err = json.Unmarshal(resp[0:n], &event)
	if err != nil {
		return nil, err
	}
	switch event.Event {
	case "pusher:error":
		var data eventError
		err = json.Unmarshal([]byte(event.Data), &data)
		if err != nil {
			return nil, err
		}
		err = errors.Errorf("Pusher return error : code : %d, message %s", data.Code, data.Message)
		return nil, err
	case "pusher:connection_established":
		sChannels := new(subscribedChannels)
		sChannels.channels = make([]string, 0)
		pClient := Client{
			ws:                 ws,
			subscribedChannels: sChannels,
			binders:            make(map[string]chan *Event),
			heartbeatStopCh:    make(chan struct{}, 1),
		}
		go pClient.heartbeat()
		go pClient.listen()
		return &pClient, nil
	}
	return nil, errors.New("Ooooops something wrong happen")
}

// NewClient initialize & return a Pusher client
func NewClient(appKey string) (*Client, error) {
	return NewCustomClient(appKey, "ws.pusherapp.com:443", "wss")
}
