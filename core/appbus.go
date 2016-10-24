package core

import (
	"errors"
	"regexp"
	"sync"
	"sync/atomic"
)

type appbuslistener struct {
	fn      func(map[string]string)
	handler EventHandler
}

type appbus struct {
	m         sync.RWMutex
	listeners map[*regexp.Regexp][]appbuslistener

	events     chan string
	Done       chan struct{}
	closed     uint64
	handlerctr uint64
}

func newAppBus() *appbus {
	bus := &appbus{
		listeners: make(map[*regexp.Regexp][]appbuslistener),
		events:    make(chan string, 128),
		Done:      make(chan struct{}, 1),
	}
	go bus.coreloop()
	return bus
}

func (bus *appbus) AddListener(expr string, listener func(map[string]string)) (EventHandler, error) {
	if bus == nil {
		return EventHandler(0), errors.New("bus is nil")
	}

	if atomic.LoadUint64(&bus.closed) > 0 {
		return EventHandler(0), errors.New("bus is closed")
	}

	bus.m.Lock()
	defer bus.m.Unlock()
	var foundKey *regexp.Regexp
	for key := range bus.listeners {
		if key.String() == expr {
			foundKey = key
			break
		}
	}

	if foundKey != nil {
		handler := atomic.AddUint64(&bus.handlerctr, 1)
		listeners := append(bus.listeners[foundKey], appbuslistener{listener, EventHandler(handler)})
		bus.listeners[foundKey] = listeners

		return EventHandler(handler), nil
	}

	re, err := regexp.Compile(expr)
	if err != nil {
		return 0, err
	}

	handler := atomic.AddUint64(&bus.handlerctr, 1)
	listeners := append(bus.listeners[re], appbuslistener{listener, EventHandler(handler)})
	bus.listeners[re] = listeners

	return EventHandler(handler), nil
}

func (bus *appbus) RemoveHandler(handler EventHandler) {
	if bus == nil {
		return
	}
	bus.m.Lock()
	defer bus.m.Unlock()

	// this is slow but is mostly here for completion purposes. if this gets used more than i think, we might have to redo
	for key, listeners := range bus.listeners {
		for i, listener := range listeners {
			if listener.handler == handler {
				bus.listeners[key] = append(listeners[:i], listeners[i+1:]...)
			}
		}

		if len(bus.listeners[key]) < 1 {
			delete(bus.listeners, key)
			break
		}

	}
}

func (bus *appbus) Emit(action string) {
	if bus == nil || atomic.LoadUint64(&bus.closed) > 0 {
		return
	}

	bus.events <- action
}

func (bus *appbus) coreloop() {
coreloop:
	for {
		select {
		case event := <-bus.events:
			bus.fireEvent(event)
		case <-bus.Done:
			atomic.StoreUint64(&bus.closed, 1)
			break coreloop
		}
	}
}

func (bus *appbus) fireEvent(event string) {
	bus.m.RLock()
	defer bus.m.RUnlock()

	// we could make this smoother by unlocking earlier and copying the slices of listeners that need to be fired
	// but it would make Remove strange, events would be fired after Remove()
	for re, listeners := range bus.listeners {
		matches, err := RegexpNamedGroupsMatch(re, event)
		if err != nil {
			continue
		}

		for _, listener := range listeners {
			listener.fn(matches)
		}
	}
}
