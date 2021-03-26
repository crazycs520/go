package runtime

const (
	TraceModeDefault   = 0
	TraceModeGoroutine = 1
	TraceModeAll       = 2
)

// GDesc contains statistics and execution details of a single goroutine.
type GDesc struct {
	ID           uint64
	Name         string
	PC           uint64
	StartTime    int64
	CreationTime int64
	EndTime      int64

	// Statistics of execution time during the goroutine execution.
	GExecutionStat

	*gdesc // private part.
}

// GExecutionStat contains statistics about a goroutine's execution
// during a period of time.
type GExecutionStat struct {
	ExecTime      int64
	SchedWaitTime int64
	IOTime        int64
	BlockTime     int64
	SyscallTime   int64
	GCTime        int64
	SweepTime     int64
	TotalTime     int64
}

// gdesc is a private part of GDesc that is required only during analysis.
type gdesc struct {
	lastStartTime    int64
	blockNetTime     int64
	blockSyncTime    int64
	blockSyscallTime int64
	blockSweepTime   int64
	blockGCTime      int64
	blockSchedTime   int64
}

// finalize is called when processing a goroutine end event or at
// the end of trace processing. This finalizes the execution stat
// and any active regions in the goroutine, in which case trigger is nil.
func (g *GDesc) finalize(endTs, activeGCStartTime int64) {
	if endTs != 0 {
		g.EndTime = endTs
	}

	finalStat := g.snapshotStat(endTs, activeGCStartTime)

	g.GExecutionStat = finalStat
	*(g.gdesc) = gdesc{}
}

func (g *GDesc) finishRemain(endTs, activeGCStartTime int64) {
	g.GExecutionStat = g.snapshotStat(endTs, activeGCStartTime)
}

// snapshotStat returns the snapshot of the goroutine execution statistics.
// This is called as we process the ordered trace event stream. lastTs and
// activeGCStartTime are used to process pending statistics if this is called
// before any goroutine end event.
func (g *GDesc) snapshotStat(lastTs, activeGCStartTime int64) (ret GExecutionStat) {
	ret = g.GExecutionStat

	if g.gdesc == nil {
		return ret // finalized GDesc. No pending state.
	}

	if activeGCStartTime != 0 { // terminating while GC is active
		if g.CreationTime < activeGCStartTime {
			ret.GCTime += lastTs - activeGCStartTime
		} else {
			// The goroutine's lifetime completely overlaps
			// with a GC.
			ret.GCTime += lastTs - g.CreationTime
		}
	}

	if g.TotalTime == 0 {
		ret.TotalTime = lastTs - g.CreationTime
	}

	if g.lastStartTime != 0 {
		ret.ExecTime += lastTs - g.lastStartTime
	}
	if g.blockNetTime != 0 {
		ret.IOTime += lastTs - g.blockNetTime
	}
	if g.blockSyncTime != 0 {
		ret.BlockTime += lastTs - g.blockSyncTime
	}
	if g.blockSyscallTime != 0 {
		ret.SyscallTime += lastTs - g.blockSyscallTime
	}
	if g.blockSchedTime != 0 {
		ret.SchedWaitTime += lastTs - g.blockSchedTime
	}
	if g.blockSweepTime != 0 {
		ret.SweepTime += lastTs - g.blockSweepTime
	}
	return ret
}

var gsStats struct {
	ps          []*pGsCollector
	global      *pGsCollector
	gcStartTime int64 // gcStartTime == 0 indicates gc is inactive.
}

type pGsCollector struct {
	gs     map[uint64]*GDesc // global Gs
	lastGs map[int]uint64    // last goroutine for global P
	lastG  uint64
	lastP  int
}

func resetGsStats(np int) {
	if np == 0 {
		gsStats.ps = nil
		gsStats.global = nil
		gsStats.gcStartTime = 0
		return
	}
	gsStats.ps = make([]*pGsCollector, np)
	for i := 0; i < np; i++ {
		gsStats.ps[i] = &pGsCollector{
			gs:     make(map[uint64]*GDesc),
			lastGs: make(map[int]uint64),
		}
	}
	gsStats.global = &pGsCollector{
		gs:     make(map[uint64]*GDesc),
		lastGs: make(map[int]uint64),
	}
	gsStats.gcStartTime = 0
}

// parse logic same with GoroutineStats in internal/trace/goroutines.go

//go:yeswritebarrierrec
func collectGStats(pid int32, ev byte, args ...uint64) {
	var collector *pGsCollector
	if pid == traceGlobProc {
		collector = gsStats.global
	} else {
		collector = gsStats.ps[pid]
	}
	gs := collector.gs
	lastGs := collector.lastGs
	lastG := collector.lastG
	lastP := collector.lastP

	ts := nanotime()

	print(EventDescriptions[ev].Name, " lastG: ", lastG, " lastP: ", lastP)
	if len(args) > 0 {
		print(" arg[0]: ", args[0])
	}
	print("\n")
	switch ev {
	case traceEvBatch:
		lastGs[lastP] = lastG
		lastP = int(args[0])
		lastG = lastGs[lastP]
	case traceEvGoCreate:
		g := &GDesc{ID: args[0], CreationTime: ts, gdesc: new(gdesc)}
		g.blockSchedTime = ts
		gs[g.ID] = g
	case traceEvGoStart, traceEvGoStartLocal, traceEvGoStartLabel:
		lastG = args[0]
		g := getGDest(pid, args[0])
		g.lastStartTime = ts
		if g.StartTime == 0 {
			g.StartTime = ts
		}
		if g.blockSchedTime != 0 {
			g.SchedWaitTime += ts - g.blockSchedTime
			g.blockSchedTime = 0
		}
	case traceEvGoEnd, traceEvGoStop:
		g := getGDest(pid, lastG)
		g.finalize(ts, gsStats.gcStartTime)
		// todo: remove g from gsStats.gs.
		lastG = 0
	case traceEvGoBlockSend, traceEvGoBlockRecv, traceEvGoBlockSelect,
		traceEvGoBlockSync, traceEvGoBlockCond:
		g := getGDest(pid, lastG)
		if g.lastStartTime != 0 {
			g.ExecTime += ts - g.lastStartTime
			g.lastStartTime = 0
		}
		g.blockSyncTime = ts
		lastG = 0
	case traceEvGoSched, traceEvGoPreempt:
		g := getGDest(pid, lastG)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockSchedTime = ts
		lastG = 0
	case traceEvGoSleep, traceEvGoBlock:
		g := getGDest(pid, lastG)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		lastG = 0
	case traceEvGoBlockNet:
		g := getGDest(pid, lastG)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockNetTime = ts
		lastG = 0
	case traceEvGoBlockGC:
		g := getGDest(pid, lastG)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockGCTime = ts
		lastG = 0
	case traceEvGoUnblock:
		g := getGDest(pid, args[0])
		if g.blockNetTime != 0 {
			g.IOTime += ts - g.blockNetTime
			g.blockNetTime = 0
		}
		if g.blockSyncTime != 0 {
			g.BlockTime += ts - g.blockSyncTime
			g.blockSyncTime = 0
		}
		g.blockSchedTime = ts
	case traceEvGoSysBlock:
		g := getGDest(pid, lastG)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockSyscallTime = ts
		lastG = 0
	case traceEvGoSysExit:
		g := getGDest(pid, args[0])
		if g.blockSyscallTime != 0 {
			g.SyscallTime += ts - g.blockSyscallTime
			g.blockSyscallTime = 0
		}
		g.blockSchedTime = ts
	case traceEvGCSweepStart:
		g := getGDest(pid, lastG)
		if g != nil {
			g.blockSweepTime = ts
		}
	case traceEvGCSweepDone:
		g := getGDest(pid, lastG)
		if g != nil && g.blockSweepTime != 0 {
			g.SweepTime += ts - g.blockSweepTime
			g.blockSweepTime = 0
		}
	case traceEvGCStart:
		gsStats.gcStartTime = ts
	case traceEvGCDone:
		// todo: update for all gs?
		for _, g := range gs {
			if g.EndTime != 0 {
				continue
			}
			if gsStats.gcStartTime < g.CreationTime {
				g.GCTime += ts - g.CreationTime
			} else {
				g.GCTime += ts - gsStats.gcStartTime
			}
		}
		gsStats.gcStartTime = 0 // indicates gc is inactive.
	}
	collector.lastG = lastG
	collector.lastP = lastP
}

func getGDest(pid int32, gid uint64) *GDesc {
	if pid != traceGlobProc {
		gs := gsStats.ps[pid].gs
		stat := gs[gid]
		if stat != nil {
			return stat
		}
	}
	// traverse all p, need lock.
	for _, collector := range gsStats.ps {
		stat := collector.gs[gid]
		if stat != nil {
			return stat
		}
	}

	// get from global
	return gsStats.global.gs[gid]
}

func GetGDesc() (s GDesc) {
	if !trace.enabled || trace.mode == TraceModeDefault {
		return s
	}
	g := getg()
	id := g.m.curg.goid
	var pid int32
	if p := g.m.p.ptr(); p != nil {
		pid = p.id
		gs := gsStats.ps[pid].gs
		stat := gs[uint64(id)]
		if stat != nil {
			s = *stat
			s.finishRemain(nanotime(), gsStats.gcStartTime)
			return s
		}
	}

	// traverse all p, need lock.
	for _, collector := range gsStats.ps {
		stat := collector.gs[uint64(id)]
		if stat != nil {
			s = *stat
			s.finishRemain(nanotime(), gsStats.gcStartTime)
			return s
		}
	}

	// get from global
	stat := gsStats.global.gs[uint64(id)]
	if stat == nil {
		return s
	}

	s = *stat
	s.finishRemain(nanotime(), gsStats.gcStartTime)
	return s
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
