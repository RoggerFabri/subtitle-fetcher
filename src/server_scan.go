package main

import (
	"fmt"
	"net/http"
	"time"
)

func (s *server) setScanStatus(msg string) {
	s.scanStatusMu.Lock()
	s.scanStatus = msg
	s.scanStatusMu.Unlock()
}

func (s *server) getScanStatus() string {
	s.scanStatusMu.RLock()
	defer s.scanStatusMu.RUnlock()
	return s.scanStatus
}

func (s *server) setScanSource(src string) {
	s.scanStatusMu.Lock()
	s.scanSource = src
	s.scanStatusMu.Unlock()
}

func (s *server) getScanSource() string {
	s.scanStatusMu.RLock()
	defer s.scanStatusMu.RUnlock()
	return s.scanSource
}

func (s *server) handleScan(w http.ResponseWriter, r *http.Request) {
	if !s.scanning.CompareAndSwap(false, true) {
		jsonError(w, "scan already in progress", http.StatusConflict)
		return
	}
	s.setScanSource("manual")
	go func() {
		defer s.scanning.Store(false)
		defer s.scanCurrent.Store(0)
		defer s.scanTotal.Store(0)
		fmt.Printf("[scan] started  root=%s\n", s.root)
		start := time.Now()
		s.setScanStatus("running")
		var lastLog time.Time
		if err := runScanWithProgressDB(s.ctx, s.db, s.root, int(s.workers.Load()), func(status string, done, total int) {
			s.setScanStatus(status)
			s.scanCurrent.Store(int64(done))
			s.scanTotal.Store(int64(total))
			now := time.Now()
			if done == total || now.Sub(lastLog) >= time.Second {
				lastLog = now
				fmt.Printf("[scan] %s\n", status)
			}
		}); err != nil {
			fmt.Printf("[scan] error: %v\n", err)
			s.setScanStatus("error: " + err.Error())
			return
		}
		fmt.Printf("[scan] done  elapsed=%s\n", time.Since(start).Round(time.Millisecond))
		s.setScanStatus("done")
	}()
	jsonOK(w, map[string]string{"status": "started"})
}

func (s *server) handleScanStatus(w http.ResponseWriter, r *http.Request) {
	jsonOK(w, map[string]any{
		"running": s.scanning.Load(),
		"status":  s.getScanStatus(),
		"source":  s.getScanSource(),
		"current": s.scanCurrent.Load(),
		"total":   s.scanTotal.Load(),
	})
}
