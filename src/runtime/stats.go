// Copyright 2021 The PingCAP Authors.
// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

import (
	"fmt"
	"time"
)

var stats struct{
	enabled       bool        // set when collect runtime statistics
}

func EnableStats() {
	stopTheWorldGC("start collecting stats")

	stats.enabled = true

	startTheWorldGC()
}

func DisableStats() {
	stats.enabled = true
}

type GStats struct {
	lastStartTime    int64
	blockNetTime     int64
	blockSyncTime    int64
	blockSyscallTime int64
	blockSweepTime   int64
	blockSchedTime   int64

	execTime      int64
	schedWaitTime int64
	ioTime        int64
	blockTime     int64
	syscallTime   int64
	sweepTime     int64
	totalTime     int64

	creationTime int64
}

func (s *GStats) ExecTime() int64 {
	return s.execTime
}

func (s *GStats) SchedWaitTime() int64 {
	return s.schedWaitTime
}

func (s *GStats) IOTime() int64 {
	return s.ioTime
}

func (s *GStats) BlockTime() int64 {
	return s.blockTime
}

func (s *GStats) SyscallTime() int64 {
	return s.syscallTime
}

func (s *GStats) TotalTime() int64 {
	return s.totalTime
}

func (s *GStats) String() string {
	return fmt.Sprintf("total: %s, exec: %s, io: %s, block: %s, syscall: %s, sched_wait: %s, gc_sweep: %s",
		time.Duration(s.totalTime).String(),
		time.Duration(s.execTime).String(),
		time.Duration(s.ioTime).String(),
		time.Duration(s.blockTime).String(),
		time.Duration(s.syscallTime).String(),
		time.Duration(s.schedWaitTime).String(),
		time.Duration(s.sweepTime).String())
}


func (s *GStats) recordGoCreate() {
	s.blockSchedTime = nanotime()
	s.creationTime = s.blockSchedTime
}

func (s *GStats) recordGoStart() {
	s.lastStartTime = nanotime()
	if s.blockSchedTime != 0 {
		s.schedWaitTime = s.lastStartTime - s.blockSchedTime
		s.blockSchedTime = 0
	}
}

func (s *GStats) recordGoSched() {
	ts := nanotime()
	s.execTime=ts - s.lastStartTime
	s.lastStartTime = 0
	s.blockSchedTime = ts
}

func (s *GStats) recordGoBlock() {
	ts := nanotime()
	s.execTime=ts - s.lastStartTime
	s.lastStartTime = 0
}

func (s *GStats) recordGoPark(traceEv byte) {
	switch traceEv {
	case traceEvGoBlockSend,traceEvGoBlockRecv,traceEvGoBlockSelect,
	traceEvGoBlockSync,traceEvGoBlockCond:
		ts := nanotime()
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0
		s.blockSyncTime = ts
	case traceEvGoStop:
		s.finalize()
	case traceEvGoBlockNet:
		ts := nanotime()
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0
		s.blockNetTime = ts
	case traceEvGoSleep,traceEvGoBlock:
		s.execTime += nanotime() - s.lastStartTime
		s.lastStartTime = 0
	case traceEvGoBlockGC:
		ts := nanotime()
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0
	}
}

func (s *GStats) recordGoUnpark() {
	ts := nanotime()
	if s.blockNetTime != 0 {
		s.ioTime += ts - s.blockNetTime
		s.blockNetTime = 0
	}
	if s.blockSyncTime != 0 {
		s.blockTime += ts - s.blockSyncTime
		s.blockSyncTime = 0
	}
	s.blockSchedTime = ts
}

func (s *GStats) recordGoSysBlock() {
	ts := nanotime()
	s.execTime += ts - s.lastStartTime
	s.lastStartTime = 0
	s.blockSyscallTime = ts
}

func (s *GStats) recordGoSysExit() {
	ts := nanotime()
	if s.blockSyscallTime != 0 {
		s.syscallTime += ts - s.blockSyscallTime
		s.blockSyscallTime = 0
	}
	s.blockSchedTime = ts
}

func (s *GStats) recordGCSweepStart() {
	s.blockSweepTime = nanotime()
}

func (s *GStats) recordGCSweepDone() {
	if  s.blockSweepTime != 0 {
		s.sweepTime += nanotime() - s.blockSweepTime
		s.blockSweepTime = 0
	}
}

func (s *GStats) recordGoEnd() {
	s.finalize()
}

func (s *GStats) finalize() {
	ts := nanotime()
	if s.creationTime != 0 {
		s.totalTime = ts - s.creationTime
	}
	if s.lastStartTime != 0 {
		s.execTime += ts - s.lastStartTime
	}
	if s.blockNetTime != 0 {
		s.ioTime += ts - s.blockNetTime
	}
	if s.blockSyncTime != 0 {
		s.blockTime += ts - s.blockSyncTime
	}
	if s.blockSyscallTime != 0 {
		s.syscallTime += ts - s.blockSyscallTime
	}
	if s.blockSchedTime != 0 {
		s.schedWaitTime += ts - s.blockSchedTime
	}
	if s.blockSweepTime != 0 {
		s.sweepTime += ts - s.blockSweepTime
	}
}

// public some runtime api.

// GetGID retrieves the goroutine (G's) ID.
func GetGID() int64 {
	return getg().m.curg.goid
}

// GetGID retrieves the goroutine (G's) runtime stats.
func GetGStats() *GStats {
	_g_ := getg().m.curg
	_g_.stats.recordGoEnd()
	return &_g_.stats
}

