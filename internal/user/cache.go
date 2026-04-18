package user

import (
	"context"
	"sync"
	"time"
)

// GroupCache 提供 "userID → Group" 的 30s TTL 缓存。
// 网关/限流/计费每次请求都会用到分组的 ratio / rpm_limit / tpm_limit,
// 每次打 DB 开销不小,这里做一层轻量 LRU-less 缓存即可(规模 <10K 用户)。
type GroupCache struct {
	dao *DAO
	ttl time.Duration

	mu    sync.RWMutex
	users map[uint64]userEntry
	groups map[uint64]groupEntry
}

type userEntry struct {
	groupID uint64
	at      time.Time
}

type groupEntry struct {
	group *Group
	at    time.Time
}

func NewGroupCache(dao *DAO, ttl time.Duration) *GroupCache {
	if ttl <= 0 {
		ttl = 30 * time.Second
	}
	return &GroupCache{
		dao:    dao,
		ttl:    ttl,
		users:  make(map[uint64]userEntry),
		groups: make(map[uint64]groupEntry),
	}
}

// OfUser 返回 userID 所在分组。首次未命中会回查 users + user_groups 两张表。
func (c *GroupCache) OfUser(ctx context.Context, userID uint64) (*Group, error) {
	now := time.Now()

	c.mu.RLock()
	ue, uok := c.users[userID]
	c.mu.RUnlock()

	var groupID uint64
	if uok && now.Sub(ue.at) <= c.ttl {
		groupID = ue.groupID
	} else {
		u, err := c.dao.GetByID(ctx, userID)
		if err != nil {
			return nil, err
		}
		groupID = u.GroupID
		c.mu.Lock()
		c.users[userID] = userEntry{groupID: groupID, at: now}
		c.mu.Unlock()
	}

	c.mu.RLock()
	ge, gok := c.groups[groupID]
	c.mu.RUnlock()
	if gok && now.Sub(ge.at) <= c.ttl {
		return ge.group, nil
	}
	g, err := c.dao.GetGroup(ctx, groupID)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.groups[groupID] = groupEntry{group: g, at: now}
	c.mu.Unlock()
	return g, nil
}
