package metrics

import (
	"time"

	"github.com/relab/hotstuff/metrics/types"
	"github.com/relab/hotstuff/modules"
)

// Ticker emits TickEvents on the data event loop.
type Ticker struct {
	mods     *modules.Modules
	tickerID int
	interval time.Duration
	lastTick time.Time
}

// NewTicker returns a new ticker.
func NewTicker(interval time.Duration) *Ticker {
	return &Ticker{interval: interval}
}

// InitModule gives the module access to the other modules.
func (t *Ticker) InitModule(mods *modules.Modules) {
	t.mods = mods
	t.tickerID = t.mods.DataEventLoop().AddTicker(t.interval, t.tick)
}

func (t *Ticker) tick(tickTime time.Time) interface{} {
	var event interface{}
	// skip the first tick so that elapsed will be nonzero
	if !t.lastTick.IsZero() {
		elapsed := tickTime.Sub(t.lastTick)
		event = types.TickEvent{
			Timestamp: tickTime,
			Elapsed:   elapsed,
		}
	}
	t.lastTick = tickTime
	return event
}