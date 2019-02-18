package database

import (
	"log"
	"sync"
	"time"
)

// MemGroupCache 群缓存
type MemGroupCache struct {
	sync.Mutex
	groups map[string]*Group
}

// NewMemGroupCache NewMemGroupCache
func NewMemGroupCache() *MemGroupCache {
	c := &MemGroupCache{
		groups: make(map[string]*Group),
	}
	go clean(c)

	return c
}

// Join 加入一个群，如果群不存在就创建一个
func (c *MemGroupCache) Join(group string, clientID string) error {
	c.Mutex.Lock()
	g, ok := c.groups[group]
	if !ok {
		c.groups[group] = &Group{
			Name:    group,
			Clients: make(map[string]bool),
		}
		g = c.groups[group]
	}
	c.Mutex.Unlock()

	g.rw.Lock()
	if _, ok := g.Clients[clientID]; !ok {
		g.Clients[clientID] = true
	}
	g.rw.Unlock()
	log.Println(clientID, "join group ", group)
	return nil
}

// Leave 退出群
func (c *MemGroupCache) Leave(group string, clientID string) error {
	if _, ok := c.groups[group]; !ok {
		return nil
	}
	g := c.groups[group]
	if _, ok := g.Clients[clientID]; ok {
		delete(g.Clients, clientID)
	}
	return nil
}

// GetGroupMembers 取群中成员
func (c *MemGroupCache) GetGroupMembers(group string) ([]string, error) {
	g, ok := c.groups[group]
	if !ok {
		return nil, nil
	}
	g.rw.RLock()
	mems := make([]string, len(g.Clients))
	index := 0
	for key := range g.Clients {
		mems[index] = key
		index++
	}
	g.rw.RUnlock()
	return mems, nil
}

func clean(c *MemGroupCache) {
	ticker := time.NewTicker(time.Hour)

	for {
		select {
		case <-ticker.C:

			for name, group := range c.groups {
				if len(group.Clients) == 0 {
					c.Mutex.Lock()
					delete(c.groups, name)
					c.Mutex.Unlock()
				}
			}
		}
	}
}
