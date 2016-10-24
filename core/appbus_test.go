package core

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppBus(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	bus := newAppBus()
	wg := sync.WaitGroup{}

	wg.Add(1)
	handler, err := bus.AddListener("test", func(names map[string]string) {
		assert.Len(names, 0)
		wg.Done()
	})
	require.NoError(err)

	bus.Emit("test")
	wg.Wait()

	bus.RemoveHandler(handler)
	assert.Len(bus.listeners, 0)
	bus.Done <- struct{}{}
	<-time.After(time.Millisecond)

	_, err = bus.AddListener("test", func(map[string]string) {})
	assert.Error(err)
}

func TestAppBusNamedGroups(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	bus := newAppBus()
	wg := sync.WaitGroup{}

	wg.Add(1)
	test1marker := "teststring"
	test2marker := "1234567890"

	handler, err := bus.AddListener(`(?P<test1>[a-z]+):(?P<test2>[0-9]+)`, func(names map[string]string) {
		assert.EqualValues(test1marker, names["test1"])
		assert.EqualValues(test2marker, names["test2"])
		wg.Done()
	})
	require.NoError(err)

	bus.Emit(fmt.Sprintf("%s:%s", test1marker, test2marker))
	wg.Wait()

	bus.RemoveHandler(handler)
	assert.Len(bus.listeners, 0)
	bus.Done <- struct{}{}
}

func TestAppBusManyListeners(t *testing.T) {
	require := require.New(t)

	bus := newAppBus()
	wg1 := sync.WaitGroup{}
	wg2 := sync.WaitGroup{}

	for i := 0; i < 10; i++ {
		wg1.Add(1)
		wg2.Add(1)
		_, err := bus.AddListener("test1", func(names map[string]string) {
			wg1.Done()
		})
		require.NoError(err)
		_, err = bus.AddListener("test2", func(names map[string]string) {
			wg2.Done()
		})
		require.NoError(err)
	}

	bus.Emit("test1")
	wg1.Wait()

	bus.Emit("test2")
	wg2.Wait()
	bus.Done <- struct{}{}
}
