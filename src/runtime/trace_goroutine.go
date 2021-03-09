package runtime

const (
	traceModeDefault   = 0
	traceModeGoroutine = 1
)

// GDesc contains statistics and execution details of a single goroutine.
type GDesc struct {
	ID           uint64
	Name         string
	PC           uint64
	CreationTime int64
	StartTime    int64
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
func (g *GDesc) finalize(lastTs, activeGCStartTime int64, endTs int64) {
	if endTs != 0 {
		g.EndTime = endTs
	}

	finalStat := g.snapshotStat(lastTs, activeGCStartTime)

	g.GExecutionStat = finalStat
	*(g.gdesc) = gdesc{}
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
	gs          map[uint64]*GDesc
	lastGs      map[int]uint64 // last goroutine running on P
	lastG       uint64
	lastP       int
	lastTs      int64
	gcStartTime int64 // gcStartTime == 0 indicates gc is inactive.
}

func resetGsStats() {
	gsStats.gs = make(map[uint64]*GDesc)
	gsStats.lastGs = make(map[int]uint64)
	gsStats.lastG = 0
	gsStats.lastP = 0
	gsStats.lastTs = 0
	gsStats.gcStartTime = 0
}

// parse logic same with GoroutineStats in internal/trace/goroutines.go

//go:yeswritebarrierrec
func collectGStats(ev byte, ts int64, args ...uint64) {
	switch ev {
	case traceEvBatch:
		gsStats.lastGs[gsStats.lastP] = gsStats.lastG
		gsStats.lastP = int(args[0])
		gsStats.lastG = gsStats.lastGs[gsStats.lastP]
		gsStats.lastTs = ts
	case traceEvGoCreate:
		gsStats.lastG = args[0]
		g := &GDesc{ID: gsStats.lastG, CreationTime: ts, gdesc: new(gdesc)}
		g.blockSchedTime = ts
		gsStats.gs[g.ID] = g
	case traceEvGoStart, traceEvGoStartLabel:
		gsStats.lastG = args[0]
		g := gsStats.gs[args[0]]
		g.lastStartTime = ts
		if g.StartTime == 0 {
			g.StartTime = ts
		}
		if g.blockSchedTime != 0 {
			g.SchedWaitTime += ts - g.blockSchedTime
			g.blockSchedTime = 0
		}
	case traceEvGoEnd, traceEvGoStop:
		g := gsStats.gs[gsStats.lastG]
		g.finalize(ts, gsStats.gcStartTime, ts)
		// todo: remove g from gsStats.gs.
		gsStats.lastG = 0
	case traceEvGoBlockSend, traceEvGoBlockRecv, traceEvGoBlockSelect,
		traceEvGoBlockSync, traceEvGoBlockCond:
		g := gsStats.gs[gsStats.lastG]
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockSyncTime = ts
		gsStats.lastG = 0
	case traceEvGoSched, traceEvGoPreempt:
		g := gsStats.gs[gsStats.lastG]
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockSchedTime = ts
		gsStats.lastG = 0
	case traceEvGoSleep, traceEvGoBlock:
		g := gsStats.gs[gsStats.lastG]
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		gsStats.lastG = 0
	case traceEvGoBlockNet:
		g := gsStats.gs[gsStats.lastG]
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockNetTime = ts
		gsStats.lastG = 0
	case traceEvGoBlockGC:
		g := gsStats.gs[gsStats.lastG]
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockGCTime = ts
		gsStats.lastG = 0
	case traceEvGoUnblock:
		g := gsStats.gs[args[0]]
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
		g := gsStats.gs[gsStats.lastG]
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockSyscallTime = ts
		gsStats.lastG = 0
	case traceEvGoSysExit:
		g := gsStats.gs[args[0]]
		if g.blockSyscallTime != 0 {
			g.SyscallTime += ts - g.blockSyscallTime
			g.blockSyscallTime = 0
		}
		g.blockSchedTime = ts
	case traceEvGCSweepStart:
		g := gsStats.gs[gsStats.lastG]
		if g != nil {
			// Sweep can happen during GC on system goroutine.
			g.blockSweepTime = ts
		}
	case traceEvGCSweepDone:
		g := gsStats.gs[gsStats.lastG]
		if g != nil && g.blockSweepTime != 0 {
			g.SweepTime += ts - g.blockSweepTime
			g.blockSweepTime = 0
		}
	case traceEvGCStart:
		gsStats.gcStartTime = ts
	case traceEvGCDone:
		for _, g := range gsStats.gs {
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
}

func GetGDesc() (s GDesc) {
	if len(gsStats.gs) == 0 {
		return s
	}
	id := getg().m.curg.goid
	stat := gsStats.gs[uint64(id)]
	if stat == nil {
		return s
	}
	s = *stat
	return s
}
