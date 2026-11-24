package main

import (
	"sync"
	"time"
)

type DeviceRegistration struct {
	Extension  string
	ContactURI string // Actual mobile IP:Port
	PNProvider string // "apns" or "fcm"
	PNToken    string // pn-prid
	PNParam    string // pn-param (e.g., iOS bundle ID / VoIP topic)
	UpdatedAt  time.Time
}

type Store struct {
	mu      sync.RWMutex
	devices map[string]*DeviceRegistration
	waiters map[string][]chan *DeviceRegistration
}

func NewStore() *Store {
	return &Store{
		devices: make(map[string]*DeviceRegistration),
		waiters: make(map[string][]chan *DeviceRegistration),
	}
}

// UpsertDevice saves the mobile client's latest Contact and wakes any held INVITEs.
func (s *Store) UpsertDevice(reg *DeviceRegistration) {
	s.mu.Lock()
	defer s.mu.Unlock()

	reg.UpdatedAt = time.Now()
	s.devices[reg.Extension] = reg

	// Notify all goroutines waiting on this extension to re-register
	if chans, ok := s.waiters[reg.Extension]; ok {
		for _, ch := range chans {
			select {
			case ch <- reg:
			default:
			}
		}
		delete(s.waiters, reg.Extension)
	}
}

func (s *Store) GetDevice(ext string) (*DeviceRegistration, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	dev, ok := s.devices[ext]
	return dev, ok
}

// WaitForRegistration blocks until the mobile app wakes up and sends a new REGISTER or timeout expires.
func (s *Store) WaitForRegistration(ext string, timeout time.Duration) (*DeviceRegistration, bool) {
	ch := make(chan *DeviceRegistration, 1)

	s.mu.Lock()
	s.waiters[ext] = append(s.waiters[ext], ch)
	s.mu.Unlock()

	select {
	case reg := <-ch:
		return reg, true
	case <-time.After(timeout):
		return nil, false
	}
}
