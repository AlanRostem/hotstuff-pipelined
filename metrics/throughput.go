package metrics

import (
	"time"

	"github.com/relab/hotstuff"

	"github.com/relab/hotstuff/eventloop"
	"github.com/relab/hotstuff/logging"
	"github.com/relab/hotstuff/metrics/types"
	"github.com/relab/hotstuff/modules"
	"google.golang.org/protobuf/types/known/durationpb"
)

func init() {
	RegisterReplicaMetric("throughput", func() any {
		return &Throughput{
			queuedBlockCount: make(map[hotstuff.Pipe]uint64),
			commitCount:      make(map[hotstuff.Pipe]uint64),
			execCount:        make(map[hotstuff.Pipe]uint64),
			commandCount:     make(map[hotstuff.Pipe]uint64),
		}
	})
}

// Throughput measures throughput in commits per second, and commands per second.
type Throughput struct {
	metricsLogger Logger
	opts          *modules.Options
	pipeCount     int

	queuedBlockCount map[hotstuff.Pipe]uint64
	commitCount      map[hotstuff.Pipe]uint64
	execCount        map[hotstuff.Pipe]uint64
	commandCount     map[hotstuff.Pipe]uint64
}

// InitModule gives the module access to the other modules.
func (t *Throughput) InitModule(mods *modules.Core, info modules.ScopeInfo) {
	var (
		eventLoop *eventloop.ScopedEventLoop
		logger    logging.Logger
	)
	mods.Get(
		&t.metricsLogger,
		&t.opts,
		&eventLoop,
		&logger,
	)

	t.pipeCount = info.ScopeCount

	eventLoop.RegisterHandler(hotstuff.ExecEvent{}, func(event any) {
		execEvent := event.(hotstuff.ExecEvent)
		t.recordCommit(execEvent.Pipe, execEvent.Commands)
	})

	eventLoop.RegisterHandler(hotstuff.CommitEvent{}, func(event any) {
		commitEvent := event.(hotstuff.CommitEvent)
		t.commitCount[commitEvent.Pipe]++
		t.queuedBlockCount[commitEvent.Pipe] += uint64(commitEvent.QueuedBlocks)
	})

	eventLoop.RegisterObserver(types.TickEvent{}, func(event any) {
		t.tick(event.(types.TickEvent))
	})

	logger.Info("Throughput metric enabled")
}

func (t *Throughput) recordCommit(pipe hotstuff.Pipe, commands int) {
	t.execCount[pipe]++
	t.commandCount[pipe] += uint64(commands)
}

func (t *Throughput) tick(tick types.TickEvent) {
	now := time.Now()

	var totalCommands uint64 = 0
	var totalCommits uint64 = 0
	var totalExecs uint64 = 0
	var maxCi hotstuff.Pipe = 1
	var start hotstuff.Pipe = hotstuff.NullPipe

	if t.pipeCount > 0 {
		maxCi = hotstuff.Pipe(t.pipeCount) + 1
		start++
	}

	for pipe := start; pipe < maxCi; pipe++ {
		event := &types.ThroughputMeasurement{
			Event:        types.NewReplicaEvent(uint32(t.opts.ID()), now),
			Execs:        t.execCount[pipe],
			Commits:      t.commitCount[pipe],
			Commands:     t.commandCount[pipe],
			Duration:     durationpb.New(now.Sub(tick.LastTick)),
			Pipe:         uint32(pipe),
			QueuedBlocks: t.queuedBlockCount[pipe],
		}
		t.metricsLogger.Log(event)
		totalCommands += t.commandCount[pipe]
		totalCommits += t.commitCount[pipe]
		totalExecs += t.execCount[pipe]
		// reset count for next tick
		t.commandCount[pipe] = 0
		t.execCount[pipe] = 0
		t.commitCount[pipe] = 0
		t.queuedBlockCount[pipe] = 0
	}

	if t.pipeCount > 0 {
		event := &types.TotalThroughputMeasurement{
			Event:     types.NewReplicaEvent(uint32(t.opts.ID()), now),
			Execs:     totalExecs,
			Commits:   totalCommits,
			Commands:  totalCommands,
			Duration:  durationpb.New(now.Sub(tick.LastTick)),
			PipeCount: uint32(t.pipeCount),
		}
		t.metricsLogger.Log(event)
	}
}
