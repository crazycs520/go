// Copyright 2021 The PingCAP Authors.
// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

var stats struct {
	enabled bool // set when collect runtime statistics
}

// EnableStats enable runtime to collect goroutine stats, such as:
// exec time, schedule wait time, network wait time, syscall time, GC sweep time.
func EnableStats() {
	stats.enabled = true
}

// EnableStats disable runtime to collect goroutine stats.
func DisableStats() {
	stats.enabled = false
}

type GStats struct {
	goid             int64
	lastStartTime    int64
	blockNetTime     int64
	blockSyncTime    int64
	blockSyscallTime int64
	blockSweepTime   int64
	blockSchedTime   int64

	// execTime is almost cpu time.
	execTime      int64
	schedWaitTime int64
	// ioTime is the network wait time.
	ioTime int64
	// blockTime is sync block time.
	blockTime int64
	// syscallTime is blocking syscall time.
	syscallTime int64
	// sweepTime is GC sweeping time.
	sweepTime int64
	totalTime int64

	creationTime int64
	endTime      int64
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

func (s *GStats) SweepTime() int64 {
	return s.sweepTime
}

func (s *GStats) TotalTime() int64 {
	return s.totalTime
}

func (s *GStats) recordGoCreate() {
	s.blockSchedTime = nanotime()
	s.creationTime = s.blockSchedTime
}

func (s *GStats) recordGoStart() {
	s.lastStartTime = nanotime()
	if s.blockSchedTime != 0 {
		s.schedWaitTime += s.lastStartTime - s.blockSchedTime
		s.blockSchedTime = 0
	}
}

func (s *GStats) recordGoSched() {
	ts := nanotime()
	if s.lastStartTime != 0 {
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0

	}
	s.blockSchedTime = ts
}

func (s *GStats) recordGoBlock() {
	ts := nanotime()
	if s.lastStartTime != 0 {
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0
	}
}

func (s *GStats) recordGoPark(traceEv byte) {
	switch traceEv {
	case traceEvGoBlockSend, traceEvGoBlockRecv, traceEvGoBlockSelect,
		traceEvGoBlockSync, traceEvGoBlockCond:
		ts := nanotime()
		if s.lastStartTime != 0 {
			s.execTime += ts - s.lastStartTime
			s.lastStartTime = 0
		}
		s.blockSyncTime = ts
	case traceEvGoStop:
		ts := nanotime()
		s.finalize(ts)
	case traceEvGoBlockNet:
		ts := nanotime()
		if s.lastStartTime != 0 {
			s.execTime += ts - s.lastStartTime
			s.lastStartTime = 0
		}
		s.blockNetTime = ts
	case traceEvGoSleep, traceEvGoBlock:
		if s.lastStartTime != 0 {
			s.execTime += nanotime() - s.lastStartTime
			s.lastStartTime = 0
		}
	case traceEvGoBlockGC:
		if s.lastStartTime != 0 {
			s.execTime += nanotime() - s.lastStartTime
			s.lastStartTime = 0
		}
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
	if s.lastStartTime != 0 {
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0
	}
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
	if s.isEnd() {
		return
	}
	s.blockSweepTime = nanotime()
}

func (s *GStats) recordGCSweepDone() {
	if s.isEnd() {
		return
	}

	if s.blockSweepTime != 0 {
		s.sweepTime += nanotime() - s.blockSweepTime
		s.blockSweepTime = 0
	}
}

func (s *GStats) isEnd() bool {
	return s.endTime > 0
}

func (s *GStats) recordGoEnd() {
	ts := nanotime()
	s.finalize(ts)
	s.endTime = ts
}

func (s *GStats) finalize(ts int64) {
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
	_g_ := getg().m.curg
	if _g_ != nil {
		return _g_.goid
	}
	return -1
}

// GetGID retrieves the goroutine (G's) runtime stats.
func GetGStats() *GStats {
	_g_ := getg().m.curg
	if _g_ != nil {
		_g_.stats.recordGoEnd()
		return &_g_.stats
	}
	return nil
}
