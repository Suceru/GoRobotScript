package GoInput

import "sync"

// HotkeyLoopController 管理一组按键的循环触发状态
type HotkeyLoopController struct {
	mu      sync.Mutex
	keys    []string
	enabled bool
	next    int
}

// NewHotkeyLoopController 创建并初始化按键控制器
func NewHotkeyLoopController(keys []string) *HotkeyLoopController {
	return &HotkeyLoopController{keys: append([]string(nil), keys...)}
}

// Toggle 切换开启/关闭状态，重置游标
func (c *HotkeyLoopController) Toggle() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.enabled = !c.enabled
	c.next = 0
	return c.enabled
}

// Enabled 返回当前是否处于启用状态
func (c *HotkeyLoopController) Enabled() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.enabled
}

// Next 获取下一个要发送的按键
func (c *HotkeyLoopController) Next() (string, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.enabled || len(c.keys) == 0 {
		return "", false
	}

	key := c.keys[c.next]
	c.next = (c.next + 1) % len(c.keys)
	return key, true
}
