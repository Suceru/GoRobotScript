// Package GoFSM provides a state machine controller for decision-making and automated transitions.
package GoFSM

import (
	"fmt"
	"sync"
	"time"
)

// StateHandler defines the execution logic for a state.
// Returns the name of the next state to transition to, or empty string to stop or stay.
type StateHandler func(ctx *Context) (nextState string, err error)

// Context stores shared state and variables across state transitions.
type Context struct {
	Data map[string]interface{}
	mu   sync.RWMutex
}

func NewContext() *Context {
	return &Context{
		Data: make(map[string]interface{}),
	}
}

func (c *Context) Set(key string, val interface{}) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.Data[key] = val
}

func (c *Context) Get(key string) interface{} {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.Data[key]
}

// Machine represents a finite state machine.
type Machine struct {
	states       map[string]StateHandler
	currentState string
	Context      *Context
	running      bool
	mu           sync.Mutex
}

func NewMachine() *Machine {
	return &Machine{
		states:  make(map[string]StateHandler),
		Context: NewContext(),
	}
}

// AddState registers a state with its handler function.
func (m *Machine) AddState(name string, handler StateHandler) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.states[name] = handler
}

// SetInitialState sets the starting state for the machine.
func (m *Machine) SetInitialState(name string) {
	m.currentState = name
}

// CurrentState returns current state name.
func (m *Machine) CurrentState() string {
	return m.currentState
}

// Step executes one step of the current state and returns the new state.
func (m *Machine) Step() (string, error) {
	m.mu.Lock()
	handler, ok := m.states[m.currentState]
	m.mu.Unlock()

	if !ok {
		return "", fmt.Errorf("state handler not found: %s", m.currentState)
	}

	nextState, err := handler(m.Context)
	if err != nil {
		return m.currentState, err
	}
	if nextState != "" {
		m.currentState = nextState
	}
	return m.currentState, nil
}

// Run executes the state machine loop until state is empty or an error occurs.
func (m *Machine) Run(interval time.Duration) error {
	m.running = true
	for m.running && m.currentState != "" {
		next, err := m.Step()
		if err != nil {
			return err
		}
		if next == "" {
			break
		}
		if interval > 0 {
			time.Sleep(interval)
		}
	}
	return nil
}

// Stop stops the running state machine.
func (m *Machine) Stop() {
	m.running = false
}
