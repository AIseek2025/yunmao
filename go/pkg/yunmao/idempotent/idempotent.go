// Package idempotent 提供进程内幂等键缓存（替代 Redis 在 PoC 阶段的角色）。
package idempotent

import (
	"container/list"
	"sync"
	"time"
)

// Cache 简单的 FIFO + TTL 幂等缓存；超过 capacity 后按插入顺序淘汰；
// 支持 TTL 过期。
type Cache struct {
	mu       sync.Mutex
	capacity int
	ttl      time.Duration
	keys     *list.List
	m        map[string]*entry
	now      func() time.Time
}

type entry struct {
	key      string
	expireAt time.Time
	elem     *list.Element
}

// NewCache 创建幂等缓存。capacity = 0 表示不限制，ttl = 0 表示永不过期（直到被踢出）。
func NewCache(capacity int, ttl time.Duration) *Cache {
	return &Cache{
		capacity: capacity,
		ttl:      ttl,
		keys:     list.New(),
		m:        make(map[string]*entry),
		now:      time.Now,
	}
}

// Insert 返回 true 表示首次见到；false 表示已存在（重复请求）。
func (c *Cache) Insert(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.now()
	if e, ok := c.m[key]; ok {
		if c.ttl == 0 || now.Before(e.expireAt) {
			return false
		}
		// 过期：删除
		c.keys.Remove(e.elem)
		delete(c.m, key)
	}
	elem := c.keys.PushBack(key)
	exp := time.Time{}
	if c.ttl > 0 {
		exp = now.Add(c.ttl)
	}
	c.m[key] = &entry{key: key, expireAt: exp, elem: elem}
	if c.capacity > 0 && c.keys.Len() > c.capacity {
		front := c.keys.Front()
		if front != nil {
			delete(c.m, front.Value.(string))
			c.keys.Remove(front)
		}
	}
	return true
}

// Has 仅查询（不插入）；过期视为没有。
func (c *Cache) Has(key string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.m[key]
	if !ok {
		return false
	}
	if c.ttl > 0 && c.now().After(e.expireAt) {
		return false
	}
	return true
}
