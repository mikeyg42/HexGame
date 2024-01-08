package cache

import (
	"context"
	"fmt"
	"hash/maphash"
	"sync/atomic"
	"time"

	hex "github.com/mikeyg42/HexGame/structures"
	"github.com/puzpuzpuz/xsync"
)

const cleanUpCheckFrequency = 1 * time.Minute
const expirationTime = 5 * time.Minute
const maxEntries = int64(500)

type MyCache struct {
	CacheMap    *xsync.MapOf[hex.CacheKey, hex.CacheValue]
	entryCount  int64 // Atomic counter for the number of entries
	cleanupChan chan struct{}
}


// isExpired checks if the cache entry is expired.
func isExpired(val hex.CacheValue) bool {
	return time.Now().UTC().UnixNano() > val.Expiration
}

func NewCache() *MyCache {
	var h maphash.Hash
	cacheMap := xsync.NewTypedMapOf[hex.CacheKey, hex.CacheValue](func(key hex.CacheKey) uint64 {
		h.Reset()
		h.WriteString(fmt.Sprintf("%v:%v", key.GameID, key.MoveCounter))
		return h.Sum64()
	})

	return &MyCache{
		CacheMap:    cacheMap,
		entryCount:  0,
		cleanupChan: make(chan struct{}),
	}
}

// Sethex.CacheValue sets a hex.CacheValue in the cache.
func (myC *MyCache) SetCacheValue(key hex.CacheKey, gameState []hex.Vertex) {
	expiration := time.Now().Add(expirationTime).UTC().UnixNano()

	myC.CacheMap.Store(key, hex.CacheValue{
		GameState:  gameState,
		Expiration: expiration,
	})

	// Increment the entry count
	atomic.AddInt64(&myC.entryCount, 1)

}

// Gethex.CacheValue retrieves a hex.CacheValue from the cache.
func (myC *MyCache) GetCacheValue(key hex.CacheKey) (hex.CacheValue, bool) {
	entry, ok := myC.CacheMap.Load(key)
	if ok && !isExpired(entry) {
		return entry, true
	}
	return hex.CacheValue{}, false
}

// Deletehex.CacheValue deletes a hex.CacheValue from the cache.
func (myC *MyCache) DeleteCacheValue(key hex.CacheKey) {
	myC.CacheMap.Delete(key)

	// Decrement the entry count
	atomic.AddInt64(&myC.entryCount, -1)
}

func (myC *MyCache) MonitorCacheSize(ctx context.Context) {
	ticker := time.NewTicker(cleanUpCheckFrequency)
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			if myC.isTooBig() {
				myC.cleanupChan <- struct{}{}
			}
		}
	}
}

func (myC *MyCache) isTooBig() bool {
	currentCount := atomic.LoadInt64(&myC.entryCount)
	return currentCount > maxEntries
}

// cleanup periodically clears expired entries and retains only the top 4 latest moves per game.
func (myC *MyCache) Cleanup(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
			
		case <-myC.cleanupChan:
			// Map to keep track of the highest move counter for each game
			highestMoves := make(map[int]int)

			// Range over the cache
			myC.CacheMap.Range(func(key hex.CacheKey, val hex.CacheValue) bool {
				if isExpired(val) {
					myC.DeleteCacheValue(key)
				} else {
					if currentHighest, found := highestMoves[key.GameID]; !found || key.MoveCounter > currentHighest {
						highestMoves[key.GameID] = key.MoveCounter
					}
				}
				return true // Continue iteration
			})

			// Remove all entries except for the one with the highest move counter for each game
			myC.CacheMap.Range(func(key hex.CacheKey, _ hex.CacheValue) bool {
				if key.MoveCounter < highestMoves[key.GameID] {
					myC.DeleteCacheValue(key)
				}
				return true // Continue iteration
			})
		}
	}
}

/*
IMPLEMENTATION:

func main() {

	myC := NewCache()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go myC.MonitorCacheSize(ctx)
	go myC.Cleanup(ctx)

	//....
	Sethex.CacheValue(hex.CacheKey{GameID: 1, MoveCounter: 3}, []hex.Vertex{{X: 3, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 4}})
}

*/
