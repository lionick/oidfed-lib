package cache

import (
	"encoding/base64"
	"log"
	"time"

	"github.com/TwiN/gocache/v2"
	"github.com/pkg/errors"
	"github.com/vmihailenco/msgpack/v5"

	"github.com/lionick/oidfed-lib/internal"
)

// Cache is an interface for caching data
type Cache interface {
	Get(key string, target any) (bool, error)
	Set(key string, value any, expiration time.Duration) error
}

// cacheWrapper is a type implementing the Cache interface and providing an
// internal cache
type cacheWrapper struct {
	c *gocache.Cache
}

func newCacheWrapper(defaultExpiration time.Duration) cacheWrapper {
	c := gocache.NewCache().WithDefaultTTL(defaultExpiration)
	if err := c.StartJanitor(); err != nil {
		log.Fatal(err) // skipcq: RVV-A0003
	}
	return cacheWrapper{
		c,
	}
}

// Get implements the Cache interface
func (c cacheWrapper) Get(key string, target any) (bool, error) {
	entryV, ok := c.c.Get(key)
	if !ok {
		return false, nil
	}
	entry, ok := entryV.([]byte)
	if !ok {
		internal.Log("invalid cache entry type")
		return false, errors.New("invalid cache entry type")
	}
	return true, msgpack.Unmarshal(entry, target)
}

// Set implements the Cache interface
func (c cacheWrapper) Set(key string, value any, expiration time.Duration) error {
	data, err := msgpack.Marshal(value)
	if err != nil {
		return err
	}
	c.c.SetWithTTL(key, data, expiration)
	return nil
}

var cacheCache Cache

func init() {
	SetCache(newCacheWrapper(time.Hour))
}

// SetCache sets the Cache that is used
func SetCache(cache Cache) {
	cacheCache = cache
}

// Constants for keys for sub caches
const (
	KeyEntityStatement            = "entity_statement"
	KeyOPMetadata                 = "op_metadata"
	KeyEntityConfiguration        = "entity_configuration"
	KeyTrustTree                  = "trust_tree"
	KeyTrustTreeChains            = "trust_tree_chains"
	KeyTrustChainResolvedMetadata = "trustchain_resolved_metadata"
	KeySubordinateListing         = "subordinate_listing"
)

// Key combines a sub system prefix with the key to a cache key
func Key(subsystem, subkey string) string {
	return subsystem + ":" + subkey
}

// EntityStmtCacheKey constructs a cache key for an EntityStatementPayload
func EntityStmtCacheKey(subID, issID string) string {
	subkey := base64.URLEncoding.EncodeToString([]byte(subID)) + ":" + base64.URLEncoding.EncodeToString([]byte(issID))
	return Key(KeyEntityStatement, subkey)
}

// Set caches a value for the given key and duration in the cache
func Set(key string, value any, duration time.Duration) error {
	return cacheCache.Set(key, value, duration)
}

// Get obtains a value for the given key from the cache
func Get(key string, target any) (bool, error) {
	return cacheCache.Get(key, target)
}
