// Copyright 2021 The PingCAP Authors.
// Copyright 2014 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package runtime

var stats struct {
	enabled bool // set when collect runtime statistics
	debug   bool
}

func EnableStats() {
	//stopTheWorldGC("start collecting stats")
	stats.enabled = true
	//startTheWorldGC()
}

func EnableStatsDebug() {
	stats.debug = true
}

func DisableStats() {
	stats.enabled = false
	stats.debug = false
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
	if stats.debug {
		print("GoCreate", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) recordGoStart() {
	s.lastStartTime = nanotime()
	if s.blockSchedTime != 0 {
		s.schedWaitTime += s.lastStartTime - s.blockSchedTime
		s.blockSchedTime = 0
	}
	if stats.debug {
		print("GoStart", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) recordGoSched() {
	ts := nanotime()
	if s.lastStartTime != 0 {
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0

	}
	s.blockSchedTime = ts
	if stats.debug {
		print("GoSched", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) recordGoBlock() {
	ts := nanotime()
	if s.lastStartTime != 0 {
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0
	}
	if stats.debug {
		print("GoBlock", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
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
	if stats.debug {
		print(EventDescriptions[traceEv].Name, ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
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
	if stats.debug {
		print("GoUnpark", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) recordGoSysBlock() {
	ts := nanotime()
	if s.lastStartTime != 0 {
		s.execTime += ts - s.lastStartTime
		s.lastStartTime = 0
	}
	s.blockSyscallTime = ts
	if stats.debug {
		print("GoSysBlock", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) recordGoSysExit() {
	ts := nanotime()
	if s.blockSyscallTime != 0 {
		s.syscallTime += ts - s.blockSyscallTime
		s.blockSyscallTime = 0
	}
	s.blockSchedTime = ts
	if stats.debug {
		print("GoSysExit", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) recordGCSweepStart() {
	if s.isEnd() {
		return
	}
	s.blockSweepTime = nanotime()
	if stats.debug {
		print("GoGCSweepStart", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) recordGCSweepDone() {
	if s.isEnd() {
		return
	}

	if s.blockSweepTime != 0 {
		s.sweepTime += nanotime() - s.blockSweepTime
		s.blockSweepTime = 0
	}
	if stats.debug {
		print("GoGCSweepDone", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) isEnd() bool {
	return s.endTime > 0
}

func (s *GStats) recordGoEnd() {
	ts := nanotime()
	s.finalize(ts)
	s.endTime = ts
	if stats.debug {
		print("GoEnd", ", go: ", s.goid, " exec: ", s.execTime/1000000, "\n")
	}
}

func (s *GStats) finalize(ts int64) {
	if s.creationTime != 0 {
		s.totalTime = ts - s.creationTime
	} else {
		s.totalTime = s.execTime + s.ioTime + s.blockTime + s.syscallTime + s.schedWaitTime + s.blockSweepTime
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

var EventDescriptions = [traceEvCount]struct {
	Name       string
	minVersion int
	Stack      bool
	Args       []string
	SArgs      []string // string arguments
}{
	traceEvNone:              {"None", 1005, false, []string{}, nil},
	traceEvBatch:             {"Batch", 1005, false, []string{"p", "ticks"}, nil}, // in 1.5 format it was {"p", "seq", "ticks"}
	traceEvFrequency:         {"Frequency", 1005, false, []string{"freq"}, nil},   // in 1.5 format it was {"freq", "unused"}
	traceEvStack:             {"Stack", 1005, false, []string{"id", "siz"}, nil},
	traceEvGomaxprocs:        {"Gomaxprocs", 1005, true, []string{"procs"}, nil},
	traceEvProcStart:         {"ProcStart", 1005, false, []string{"thread"}, nil},
	traceEvProcStop:          {"ProcStop", 1005, false, []string{}, nil},
	traceEvGCStart:           {"GCStart", 1005, true, []string{"seq"}, nil}, // in 1.5 format it was {}
	traceEvGCDone:            {"GCDone", 1005, false, []string{}, nil},
	traceEvGCSTWStart:        {"GCSTWStart", 1005, false, []string{"kindid"}, []string{"kind"}}, // <= 1.9, args was {} (implicitly {0})
	traceEvGCSTWDone:         {"GCSTWDone", 1005, false, []string{}, nil},
	traceEvGCSweepStart:      {"GCSweepStart", 1005, true, []string{}, nil},
	traceEvGCSweepDone:       {"GCSweepDone", 1005, false, []string{"swept", "reclaimed"}, nil}, // before 1.9, format was {}
	traceEvGoCreate:          {"GoCreate", 1005, true, []string{"g", "stack"}, nil},
	traceEvGoStart:           {"GoStart", 1005, false, []string{"g", "seq"}, nil}, // in 1.5 format it was {"g"}
	traceEvGoEnd:             {"GoEnd", 1005, false, []string{}, nil},
	traceEvGoStop:            {"GoStop", 1005, true, []string{}, nil},
	traceEvGoSched:           {"GoSched", 1005, true, []string{}, nil},
	traceEvGoPreempt:         {"GoPreempt", 1005, true, []string{}, nil},
	traceEvGoSleep:           {"GoSleep", 1005, true, []string{}, nil},
	traceEvGoBlock:           {"GoBlock", 1005, true, []string{}, nil},
	traceEvGoUnblock:         {"GoUnblock", 1005, true, []string{"g", "seq"}, nil}, // in 1.5 format it was {"g"}
	traceEvGoBlockSend:       {"GoBlockSend", 1005, true, []string{}, nil},
	traceEvGoBlockRecv:       {"GoBlockRecv", 1005, true, []string{}, nil},
	traceEvGoBlockSelect:     {"GoBlockSelect", 1005, true, []string{}, nil},
	traceEvGoBlockSync:       {"GoBlockSync", 1005, true, []string{}, nil},
	traceEvGoBlockCond:       {"GoBlockCond", 1005, true, []string{}, nil},
	traceEvGoBlockNet:        {"GoBlockNet", 1005, true, []string{}, nil},
	traceEvGoSysCall:         {"GoSysCall", 1005, true, []string{}, nil},
	traceEvGoSysExit:         {"GoSysExit", 1005, false, []string{"g", "seq", "ts"}, nil},
	traceEvGoSysBlock:        {"GoSysBlock", 1005, false, []string{}, nil},
	traceEvGoWaiting:         {"GoWaiting", 1005, false, []string{"g"}, nil},
	traceEvGoInSyscall:       {"GoInSyscall", 1005, false, []string{"g"}, nil},
	traceEvHeapAlloc:         {"HeapAlloc", 1005, false, []string{"mem"}, nil},
	traceEvNextGC:            {"NextGC", 1005, false, []string{"mem"}, nil},
	traceEvTimerGoroutine:    {"TimerGoroutine", 1005, false, []string{"g"}, nil}, // in 1.5 format it was {"g", "unused"}
	traceEvFutileWakeup:      {"FutileWakeup", 1005, false, []string{}, nil},
	traceEvString:            {"String", 1007, false, []string{}, nil},
	traceEvGoStartLocal:      {"GoStartLocal", 1007, false, []string{"g"}, nil},
	traceEvGoUnblockLocal:    {"GoUnblockLocal", 1007, true, []string{"g"}, nil},
	traceEvGoSysExitLocal:    {"GoSysExitLocal", 1007, false, []string{"g", "ts"}, nil},
	traceEvGoStartLabel:      {"GoStartLabel", 1008, false, []string{"g", "seq", "labelid"}, []string{"label"}},
	traceEvGoBlockGC:         {"GoBlockGC", 1008, true, []string{}, nil},
	traceEvGCMarkAssistStart: {"GCMarkAssistStart", 1009, true, []string{}, nil},
	traceEvGCMarkAssistDone:  {"GCMarkAssistDone", 1009, false, []string{}, nil},
	traceEvUserTaskCreate:    {"UserTaskCreate", 1011, true, []string{"taskid", "pid", "typeid"}, []string{"name"}},
	traceEvUserTaskEnd:       {"UserTaskEnd", 1011, true, []string{"taskid"}, nil},
	traceEvUserRegion:        {"UserRegion", 1011, true, []string{"taskid", "mode", "typeid"}, []string{"name"}},
	traceEvUserLog:           {"UserLog", 1011, true, []string{"id", "keyid"}, []string{"category", "message"}},
}
