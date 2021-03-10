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
	globalGs     map[uint64]*GDesc   // global Gs
	pgs          []map[uint64]*GDesc // gs for each P
	globalLastGs map[int]uint64      // last goroutine for global P
	pLastGs      []map[int]uint64    // last goroutine running on P

	lastGs      []uint64
	lastPs      []int
	gcStartTime int64 // gcStartTime == 0 indicates gc is inactive.
}

func resetGsStats(np int) {
	gsStats.globalGs = make(map[uint64]*GDesc)
	gsStats.globalLastGs = make(map[int]uint64)

	gsStats.pgs = make([]map[uint64]*GDesc, np)
	gsStats.pLastGs = make([]map[int]uint64, np)
	for i := 0; i < np; i++ {
		gsStats.pgs[i] = make(map[uint64]*GDesc)
		gsStats.pLastGs[i] = make(map[int]uint64)
	}
	gsStats.lastGs = make([]uint64, np+1)
	gsStats.lastPs = make([]int, np+1)
	gsStats.gcStartTime = 0
}

// parse logic same with GoroutineStats in internal/trace/goroutines.go

//go:yeswritebarrierrec
func collectGStats(pid int32, ev byte, ts int64, args ...uint64) {
	var gs map[uint64]*GDesc
	var lastGs map[int]uint64
	var lastG uint64
	var lastP int
	if pid == traceGlobProc {
		gs = gsStats.globalGs
		lastGs = gsStats.globalLastGs
		lastG = gsStats.lastGs[len(gsStats.lastGs)-1]
		lastP = gsStats.lastPs[len(gsStats.lastPs)-1]
	} else {
		gs = gsStats.pgs[pid]
		lastGs = gsStats.pLastGs[pid]
		lastG = gsStats.lastGs[pid]
		lastP = gsStats.lastPs[pid]
	}

	switch ev {
	case traceEvBatch:
		lastGs[lastP] = lastG
		if pid == traceGlobProc {
			gsStats.lastPs[len(gsStats.lastPs)-1] = int(args[0])
			gsStats.lastGs[len(gsStats.lastGs)-1] = lastGs[lastP]
		} else {
			gsStats.lastPs[pid] = int(args[0])
			gsStats.lastGs[pid] = lastGs[lastP]
		}
	case traceEvGoCreate:
		setLastG(pid, args[0])
		g := &GDesc{ID: args[0], CreationTime: ts, gdesc: new(gdesc)}
		g.blockSchedTime = ts
		setgDest(pid, g.ID, g)
	case traceEvGoStart, traceEvGoStartLabel:
		setLastG(pid, args[0])
		g := getgDest(args[0], pid)
		g.lastStartTime = ts
		if g.StartTime == 0 {
			g.StartTime = ts
		}
		if g.blockSchedTime != 0 {
			g.SchedWaitTime += ts - g.blockSchedTime
			g.blockSchedTime = 0
		}
	case traceEvGoEnd, traceEvGoStop:
		g := getgDest(lastG, pid)
		g.finalize(ts, gsStats.gcStartTime, ts)
		// todo: remove g from gsStats.gs.
		setLastG(pid, 0)
	case traceEvGoBlockSend, traceEvGoBlockRecv, traceEvGoBlockSelect,
		traceEvGoBlockSync, traceEvGoBlockCond:
		g := getgDest(lastG, pid)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockSyncTime = ts
		setLastG(pid, 0)
	case traceEvGoSched, traceEvGoPreempt:
		g := getgDest(lastG, pid)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockSchedTime = ts
		setLastG(pid, 0)
	case traceEvGoSleep, traceEvGoBlock:
		g := getgDest(lastG, pid)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		setLastG(pid, 0)
	case traceEvGoBlockNet:
		g := getgDest(lastG, pid)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockNetTime = ts
		setLastG(pid, 0)
	case traceEvGoBlockGC:
		g := getgDest(lastG, pid)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockGCTime = ts
		setLastG(pid, 0)
	case traceEvGoUnblock:
		g := getgDest(args[0], pid)
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
		g := getgDest(lastG, pid)
		g.ExecTime += ts - g.lastStartTime
		g.lastStartTime = 0
		g.blockSyscallTime = ts
		setLastG(pid, 0)
	case traceEvGoSysExit:
		g := getgDest(args[0], pid)
		if g.blockSyscallTime != 0 {
			g.SyscallTime += ts - g.blockSyscallTime
			g.blockSyscallTime = 0
		}
		g.blockSchedTime = ts
	case traceEvGCSweepStart:
		g := getgDest(lastG, pid)
		if g != nil {
			// Sweep can happen during GC on system goroutine.
			g.blockSweepTime = ts
		}
	case traceEvGCSweepDone:
		g := getgDest(lastG, pid)
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
}

func setLastG(pid int32, gid uint64) {
	if pid == traceGlobProc {
		gsStats.lastGs[len(gsStats.lastGs)-1] = 0
	} else {
		gsStats.lastGs[pid] = 0
	}
}

func setgDest(pid int32, gid uint64, stat *GDesc) {
	if pid == traceGlobProc {
		gsStats.globalGs[gid] = stat
	} else {
		gsStats.pgs[pid][gid] = stat
	}
}

func getgDest(gid uint64, pid int32) *GDesc {
	if pid != traceGlobProc {
		gs := gsStats.pgs[pid]
		stat := gs[gid]
		if stat != nil {
			return stat
		}
	}

	// traverse all p, need lock.
	for _, gs := range gsStats.pgs {
		stat := gs[gid]
		if stat != nil {
			return stat
		}
	}

	// get from global
	return gsStats.globalGs[gid]
}

func GetGDesc() (s GDesc) {
	if !trace.enabled || trace.mode != traceModeDefault {
		return s
	}
	g := getg()
	id := g.m.curg.goid
	var pid int32
	if p := g.m.p.ptr(); p != nil {
		pid = p.id
		gs := gsStats.pgs[pid]
		stat := gs[uint64(id)]
		if stat != nil {
			s = *stat
			return s
		}
	}

	// traverse all p, need lock.
	for _, gs := range gsStats.pgs {
		stat := gs[uint64(id)]
		if stat != nil {
			s = *stat
			return s
		}
	}

	// get from global
	stat := gsStats.globalGs[uint64(id)]
	if stat == nil {
		return s
	}
	s = *stat
	return s
}
