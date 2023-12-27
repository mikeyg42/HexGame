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
const maxEntries = int64(1000)

type MyCache struct {
	cacheMap    *xsync.MapOf[CacheKey, CacheValue]
	entryCount  int64 // Atomic counter for the number of entries
	cleanupChan chan struct{}
}

// CacheKey represents the key for each cache entry.
type CacheKey struct {
	GameID      int
	MoveCounter int
}

// CacheValue represents a single entry in our cache.
type CacheValue struct {
	GameState  []hex.Vertex
	Expiration int64
}

// isExpired checks if the cache entry is expired.
func (ce CacheValue) isExpired() bool {
	return time.Now().UTC().UnixNano() > ce.Expiration
}

func NewCache() *MyCache {
	var h maphash.Hash
	cacheMap := xsync.NewTypedMapOf[CacheKey, CacheValue](func(key CacheKey) uint64 {
		h.Reset()
		h.WriteString(fmt.Sprintf("%v:%v", key.GameID, key.MoveCounter))
		return h.Sum64()
	})

	return &MyCache{
		cacheMap:    cacheMap,
		entryCount:  0,
		cleanupChan: make(chan struct{}),
	}
}

// SetCacheValue sets a CacheValue in the cache.
func (myC *MyCache) SetCacheValue(key CacheKey, gameState []hex.Vertex) {
	expiration := time.Now().Add(expirationTime).UTC().UnixNano()

	myC.cacheMap.Store(key, CacheValue{
		GameState:  gameState,
		Expiration: expiration,
	})

	// Increment the entry count
	atomic.AddInt64(&myC.entryCount, 1)

}

// GetCacheValue retrieves a CacheValue from the cache.
func (myC *MyCache) GetCacheValue(key CacheKey) (CacheValue, bool) {
	entry, ok := myC.cacheMap.Load(key)
	if ok && !entry.isExpired() {
		return entry, true
	}
	return CacheValue{}, false
}

// DeleteCacheValue deletes a CacheValue from the cache.
func (myC *MyCache) DeleteCacheValue(key CacheKey) {
	myC.cacheMap.Delete(key)

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
			if myC.IsTooBig() {
				myC.cleanupChan <- struct{}{}
			}
		}
	}
}

func (myC *MyCache) IsTooBig() bool {
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
			myC.cacheMap.Range(func(key CacheKey, val CacheValue) bool {
				if val.isExpired() {
					myC.DeleteCacheValue(key)
				} else {
					if currentHighest, found := highestMoves[key.GameID]; !found || key.MoveCounter > currentHighest {
						highestMoves[key.GameID] = key.MoveCounter
					}
				}
				return true // Continue iteration
			})

			// Remove all entries except for the one with the highest move counter for each game
			myC.cacheMap.Range(func(key CacheKey, _ CacheValue) bool {
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
	SetCacheValue(CacheKey{GameID: 1, MoveCounter: 3}, []hex.Vertex{{X: 3, Y: 2}, {X: 2, Y: 2}, {X: 3, Y: 4}})
}

*/
