// Package blockchain provides an implementation of the consensus.BlockChain interface.
package blockchain

import (
	"context"
	"sync"

	"github.com/relab/hotstuff"
	"github.com/relab/hotstuff/eventloop"
	"github.com/relab/hotstuff/logging"
	"github.com/relab/hotstuff/modules"
	"github.com/relab/hotstuff/pipelining"
	"github.com/relab/hotstuff/synchronizer"
)

// blockChain stores a limited amount of blocks in a map.
// blocks are evicted in LRU order.
type pipedBlockChain struct {
	configuration modules.Configuration
	// consensus     modules.Consensus
	eventLoop *eventloop.EventLoop
	logger    logging.Logger

	mut           sync.Mutex
	pruneHeight   hotstuff.View
	blocks        map[hotstuff.Hash]*hotstuff.Block
	blockAtHeight map[pipelining.PipeId]map[hotstuff.View]*hotstuff.Block
	pendingFetch  map[hotstuff.Hash]context.CancelFunc // allows a pending fetch operation to be canceled
	pipes         []pipelining.PipeId
}

func (chain *pipedBlockChain) InitModule(mods *modules.Core, _ modules.InitOptions) {
	mods.Get(
		&chain.configuration,
		// &chain.consensus,
		&chain.eventLoop,
		&chain.logger,
	)
}

// New creates a new blockChain with a maximum size.
// Blocks are dropped in least recently used order.
func NewPiped(pipes []pipelining.PipeId) modules.BlockChain {
	bc := &pipedBlockChain{
		blocks:        make(map[hotstuff.Hash]*hotstuff.Block),
		blockAtHeight: make(map[pipelining.PipeId]map[hotstuff.View]*hotstuff.Block),
		pendingFetch:  make(map[hotstuff.Hash]context.CancelFunc),
		pipes:         pipes,
	}

	for _, pid := range pipes {
		bc.blockAtHeight[pid] = make(map[hotstuff.View]*hotstuff.Block)
	}

	bc.Store(hotstuff.GetGenesis())
	return bc
}

// Store stores a block in the blockchain
func (chain *pipedBlockChain) Store(block *hotstuff.Block) {
	chain.mut.Lock()
	defer chain.mut.Unlock()

	chain.blocks[block.Hash()] = block
	chain.blockAtHeight[block.Pipe()][block.View()] = block

	// cancel any pending fetch operations
	if cancel, ok := chain.pendingFetch[block.Hash()]; ok {
		cancel()
	}
}

// Get retrieves a block given its hash. It will only try the local cache.
func (chain *pipedBlockChain) LocalGet(hash hotstuff.Hash) (*hotstuff.Block, bool) {
	chain.mut.Lock()
	defer chain.mut.Unlock()

	block, ok := chain.blocks[hash]
	if !ok {
		return nil, false
	}

	return block, true
}

// Get retrieves a block given its hash. Get will try to find the block locally.
// If it is not available locally, it will try to fetch the block.
func (chain *pipedBlockChain) Get(hash hotstuff.Hash) (block *hotstuff.Block, ok bool) {
	// need to declare vars early, or else we won't be able to use goto
	var (
		ctx    context.Context
		cancel context.CancelFunc
	)

	chain.mut.Lock()
	block, ok = chain.blocks[hash]
	if ok {
		goto done
	}

	ctx, cancel = synchronizer.TimeoutContext(chain.eventLoop.Context(), chain.eventLoop)
	chain.pendingFetch[hash] = cancel

	chain.mut.Unlock()
	chain.logger.Debugf("Attempting to fetch block: %.8s", hash)
	block, ok = chain.configuration.Fetch(ctx, hash)
	chain.mut.Lock()

	delete(chain.pendingFetch, hash)
	if !ok {
		// check again in case the block arrived while we we fetching
		block, ok = chain.blocks[hash]
		goto done
	}

	chain.logger.Debugf("Successfully fetched block: %.8s", hash)

	chain.blocks[hash] = block
	chain.blockAtHeight[block.Pipe()][block.View()] = block

done:
	chain.mut.Unlock()

	if !ok {
		return nil, false
	}

	return block, true
}

// Extends checks if the given block extends the branch of the target block.
func (chain *pipedBlockChain) Extends(block, target *hotstuff.Block) bool {
	current := block
	ok := true
	for ok && current.View() > target.View() {
		current, ok = chain.Get(current.Parent())
	}
	return ok && current.Hash() == target.Hash()
}

func (chain *pipedBlockChain) PruneToHeight(height hotstuff.View) (forkedBlocks []*hotstuff.Block) {
	chain.mut.Lock()
	defer chain.mut.Unlock()

	committedHeight := height // chain.consensus.CommittedBlock().View()

	for _, pid := range chain.pipes {
		committedViews := make(map[hotstuff.View]bool)
		committedViews[committedHeight] = true
		for h := committedHeight; h >= chain.pruneHeight; {
			block, ok := chain.blockAtHeight[pid][h]
			if !ok {
				break
			}
			parent, ok := chain.blocks[block.Parent()]
			if !ok || parent.View() < chain.pruneHeight {
				break
			}
			h = parent.View()
			committedViews[h] = true
		}

		for h := height; h > chain.pruneHeight; h-- {
			if !committedViews[h] {
				block, ok := chain.blockAtHeight[pid][h]
				if ok {
					chain.logger.Debugf("PruneToHeight: found forked block: %v", block)
					forkedBlocks = append(forkedBlocks, block)
				}
			}
			delete(chain.blockAtHeight[pid], h)
		}
		chain.pruneHeight = height
	}
	return forkedBlocks
}

var _ modules.BlockChain = (*blockChain)(nil)
