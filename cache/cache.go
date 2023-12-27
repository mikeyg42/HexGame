package cache
// courtesy of  Adam Szpilewicz -- https://medium.com/gitconnected/thread-safe-cache-in-go-with-sync-map-27d2a22f3201
import (
 "github.com/puzpuzpuz/xsync"
 "time"
)
const cleanUpFrequency = 1 * time.Minute
const expirationTime = 5 * time.Minute

// CacheEntry is a value stored in the cache.
type CacheEntry struct {
 gameID      string
 moveCounter int
 allMovesList []string
}

// MyCache is a thread-safe cache.
type MyCache struct {
 cacheMap sync.Map
}

func NewCache() *MyCache {
	return &MyCache{}
}

// Set stores a value in the cache with a given TTL
// (time to live) in seconds.
func (myC *MyCache) Set(key string, value interface{}) {
	expiration := time.Now().Add(expirationTime).UnixNano()
	myC.syncMap.Store(key, CacheEntry{value: value, expiration: expiration})
}

// Get retrieves a value from the cache. If the value is not found
// or has expired, it returns false.
func (myC *MyCache) Get(key string) (interface{}, bool) {

	entry, found := myC.syncMap.Load(key)
	if !found {
	return nil, false
	}

// Type assertion to CacheEntry, as entry is an interface{}
	cacheEntry := entry.(CacheEntry)

	return cacheEntry.value, true
}

// Delete removes a value from the cache.
func (myC *MyCache) Delete(key string) {
 myC.syncMap.Delete(key)
}

// CleanUp periodically removes expired entries from the cache.
func (myC *MyCache) CleanUp() {
 for {
  time.Sleep(cleanUpFrequency)
  myC.syncMap.Range(func(key, entry interface{}) bool {
   cacheEntry := entry.(CacheEntry)
   if time.Now().UnixNano() > cacheEntry.expiration {
    myC.syncMap.Delete(key)
   }
   return true
  })
 }
}
// usage:
func StartCache() {

MyCache := NewCache()
 
// Start a goroutine to periodically clean up the cache
 go MyCache.CleanUp()

 cacheKey := fmt.Sprintf("game - %d", gameID)
 MyCache.Set(cacheKey, result, 1*time.Minute)
 

 // Set a value in the cache
  cachedResult, found := MyCache.Get(cacheKey)
  
  var result int
  if found {
   result = cachedResult.(int)
  } else {
   result = expensiveComputation(n)
   MyCache.Set(cacheKey, result, 1*time.Minute)
  }
