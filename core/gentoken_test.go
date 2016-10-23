package core

import (
	"sync"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGenerateTokenUniqueness(t *testing.T) {
	assert := assert.New(t)
	var ids [1000]string
	atomic.StoreUint64(&ctr, 0)
	wg := sync.WaitGroup{}

	for i := range ids {
		wg.Add(1)
		go func(index int) {
			ids[index] = generateToken()
			wg.Done()
		}(i)
	}

	wg.Wait()
	assert.Equal(uint64(len(ids)), ctr, "Counter should have incremented as many times as we asked for ids")

	for i, id := range ids {
		// make sure that this id isn't repeated in the rest of the ids
		assert.NotContains(ids[:i], id)
		assert.NotContains(ids[i+1:], id)
	}
}
