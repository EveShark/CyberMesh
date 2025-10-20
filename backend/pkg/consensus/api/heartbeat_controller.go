package api

import "sync"

type activationHeartbeatController struct {
	mu            sync.RWMutex
	senderEnabled bool
}

func (c *activationHeartbeatController) ShouldStartListener() bool {
	return true
}

func (c *activationHeartbeatController) ShouldStartSender() bool {
	if c == nil {
		return false
	}
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.senderEnabled
}

func (c *activationHeartbeatController) EnableSender() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.senderEnabled = true
	c.mu.Unlock()
}

func (c *activationHeartbeatController) Reset() {
	if c == nil {
		return
	}
	c.mu.Lock()
	c.senderEnabled = false
	c.mu.Unlock()
}
