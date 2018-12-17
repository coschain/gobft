package go_bft

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	"github.com/coschain/go-bft/common"
	"github.com/coschain/go-bft/custom"
	"github.com/coschain/go-bft/message"
	log "github.com/sirupsen/logrus"
)

type Core struct {
	cfg        *Config
	validators *Validators

	RoundState
	triggeredTimeoutPrecommit bool

	msgQueue      chan msgInfo
	timeoutTicker TimeoutTicker
	done          chan struct{}

	sync.RWMutex
}

func New(vals custom.IValidators, pVal custom.IPrivValidator) *Core {
	c := &Core{
		cfg:           DefaultConfig(),
		validators:    NewValidators(vals, pVal),
		msgQueue:      make(chan msgInfo, msgQueueSize),
		timeoutTicker: NewTimeoutTicker(),
		done:          make(chan struct{}),
	}

	appState := vals.GetAppState()
	c.reconstructLastCommit()
	c.updateToAppState(appState)
	return c
}

func (c *Core) Start() error {
	if err := c.timeoutTicker.Start(); err != nil {
		return err
	}

	go c.receiveRoutine()
	c.scheduleRound0(c.GetRoundState())
	return nil
}

func (c *Core) Stop() error {
	close(c.done)
	return nil
}

// GetRoundState returns a shallow copy of the internal consensus state.
func (c *Core) GetRoundState() *RoundState {
	c.RLock()
	rs := c.RoundState // copy
	c.RUnlock()
	return &rs
}

// enterNewRound(height, 0) at c.StartTime.
func (c *Core) scheduleRound0(rs *RoundState) {
	log.Info("scheduleRound0", "now", common.Now(), "startTime", c.StartTime)
	sleepDuration := rs.StartTime.Sub(common.Now()) // nolint: gotype, gosimple
	c.scheduleTimeout(sleepDuration, rs.Height, 0, RoundStepNewHeight)
}

func (c *Core) updateRoundStep(round int, step RoundStepType) {
	c.Round = round
	c.Step = step
}

func (c *Core) updateToAppState(appState *message.AppState) {

}

func (c *Core) reconstructLastCommit() {

}

// receiveRoutine keeps the RoundState and is the only thing that updates it.
// Updates (state transitions) happen on timeouts, complete proposals, and 2/3 majorities.
// Core must be locked before any internal state is updated.
func (c *Core) receiveRoutine() {
	onExit := func(c *Core) {
		close(c.done)
	}

	defer func() {
		if r := recover(); r != nil {
			log.Error("CONSENSUS FAILURE!!!", "err", r, "stack", string(debug.Stack()))
			// stop gracefully
			//
			// NOTE: We most probably shouldn't be running any further when there is
			// some unexpected panic. Some unknown error happened, and so we don't
			// know if that will result in the validator signing an invalid thing. It
			// might be worthwhile to explore a mechanism for manual resuming via
			// some console or secure RPC system, but for now, halting the chain upon
			// unexpected consensus bugs sounds like the better option.
			onExit(c)
		}
	}()

	for {
		rs := c.RoundState
		var mi msgInfo

		select {
		case mi = <-c.msgQueue:
			c.handleMsg(mi)
		case ti := <-c.timeoutTicker.Chan(): // tockChan:
			c.handleTimeout(ti, rs)
		case <-c.done:
			return
		}
	}
}

func (c *Core) handleMsg(mi msgInfo) {
	c.Lock()
	defer c.Unlock()

	var err error
	msg := mi.Msg
	switch msg := msg.(type) {
	case *message.VoteMessage:
		// attempt to add the vote and dupeout the validator if its a duplicate signature
		// if the vote gives us a 2/3-any or 2/3-one, we transition
		//var added bool
		_, err = c.tryAddVote(msg.Vote)

		if err == ErrAddingVote {
			// TODO: punish peer
			// We probably don't want to stop the peer here. The vote does not
			// necessarily comes from a malicious peer but can be just broadcasted by
			// a typical peer.
			// https://github.com/tendermint/tendermint/issues/1281
		}

		// NOTE: the vote is broadcast to peers by the reactor listening
		// for vote events

		// TODO: If rs.Height == vote.Height && rs.Round < vote.Round,
		// the peer is sending us CatchupCommit precommits.
		// We could make note of this and help filter in broadcastHasVoteMessage().
	default:
		log.Error("Unknown msg type", reflect.TypeOf(msg))
	}
	if err != nil {
		log.Error("Error with msg", "height", c.Height, "round", c.Round, "type", reflect.TypeOf(msg), "err", err, "msg", msg)
	}
}

func (c *Core) handleTimeout(ti timeoutInfo, rs RoundState) {
	log.Debug("Received tock", "timeout", ti.Duration, "height", ti.Height, "round", ti.Round, "step", ti.Step)

	// timeouts must be for current height, round, step
	if ti.Height != rs.Height || ti.Round < rs.Round || (ti.Round == rs.Round && ti.Step < rs.Step) {
		log.Debug("Ignoring tock because we're ahead", "height", rs.Height, "round", rs.Round, "step", rs.Step)
		return
	}

	// the timeout will now cause a state transition
	c.Lock()
	defer c.Unlock()

	switch ti.Step {
	case RoundStepNewHeight:
		// NewRound event fired from enterNewRound.
		// XXX: should we fire timeout here (for timeout commit)?
		c.enterNewRound(ti.Height, 0)
	case RoundStepNewRound:
		c.enterPropose(ti.Height, 0)
	case RoundStepPropose:
		c.enterPrevote(ti.Height, ti.Round)
	case RoundStepPrevoteWait:
		c.enterPrecommit(ti.Height, ti.Round)
	case RoundStepPrecommitWait:
		c.enterPrecommit(ti.Height, ti.Round)
		c.enterNewRound(ti.Height, ti.Round+1)
	default:
		panic(fmt.Sprintf("Invalid timeout step: %v", ti.Step))
	}

}

// State functions
// Used internally by handleTimeout and handleMsg to make state transitions

// Enter: `timeoutNewHeight` by startTime (commitTime+timeoutCommit),
// 	or, if SkipTimeout==true, after receiving all precommits from (height,round-1)
// Enter: `timeoutPrecommits` after any +2/3 precommits from (height,round-1)
// Enter: +2/3 precommits for nil at (height,round-1)
// Enter: +2/3 prevotes any or +2/3 precommits for block or any from (height, round)
// NOTE: c.StartTime was already set for height.
func (c *Core) enterNewRound(height int64, round int) {
	if c.Height != height || round < c.Round || (c.Round == round && c.Step != RoundStepNewHeight) {
		log.Debug(fmt.Sprintf("enterNewRound(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, c.Height, c.Round, c.Step))
		return
	}

	if now := common.Now(); c.StartTime.After(now) {
		log.Info("Need to set a buffer and log message here for sanity.", "startTime", c.StartTime, "now", now)
	}

	log.Info(fmt.Sprintf("enterNewRound(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

	// Setup new round
	// we don't fire newStep for this step,
	// but we fire an event, so update the round step first
	c.updateRoundStep(round, RoundStepNewRound)
	if round == 0 {
		// We've already reset these upon new height,
		// and meanwhile we might have received a proposal
		// for round 0.
	} else {
		log.Infof("Resetting Proposal info, height %d, round %d", height, round)
		c.Proposal = nil
	}
	c.Votes.SetRound(round + 1) // also track next round (round+1) to allow round-skipping
	c.triggeredTimeoutPrecommit = false

	c.enterPropose(height, round)
}

func (c *Core) isReadyToPrevote() bool {
	if c.Proposal != nil || c.LockedRound >= 0 {
		return true
	}

	return false

}

// Enter (CreateEmptyBlocks): from enterNewRound(height,round)
// Enter (CreateEmptyBlocks, CreateEmptyBlocksInterval > 0 ): after enterNewRound(height,round), after timeout of CreateEmptyBlocksInterval
// Enter (!CreateEmptyBlocks) : after enterNewRound(height,round), once txs are in the mempool
func (c *Core) enterPropose(height int64, round int) {
	if c.Height != height || round < c.Round || (c.Round == round && RoundStepPropose <= c.Step) {
		log.Debug(fmt.Sprintf("enterPropose(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, c.Height, c.Round, c.Step))
		return
	}
	log.Info(fmt.Sprintf("enterPropose(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

	defer func() {
		// Done enterPropose:
		c.updateRoundStep(round, RoundStepPropose)

		// If we have the whole proposal + POL, then goto Prevote now.
		// else, we'll enterPrevote when the rest of the proposal is received (in AddProposalBlockPart),
		// or else after timeoutPropose
		if c.isReadyToPrevote() {
			c.enterPrevote(height, c.Round)
		}
	}()

	// If we don't get the proposal quick enough, enterPrevote
	c.scheduleTimeout(c.cfg.Propose(round), height, round, RoundStepPropose)

	self := c.validators.GetSelfPubKey()
	// Nothing more to do if we're not a validator
	if !c.validators.CustomValidators.IsValidator(self) {
		log.Debug("This node is not a validator")
		return
	}

	log.Debug("This node is a validator")

	if c.validators.CustomValidators.GetCurrentProposer() == self {
		log.Info("enterPropose: Our turn to propose", "proposer", self)
		c.doPropose(height, round)
	} else {
		log.Info("enterPropose: Not our turn to propose", "proposer",
			c.validators.CustomValidators.GetCurrentProposer(), "self", self)
	}
}

func (c *Core) doPropose(height int64, round int) {
	data := c.validators.CustomValidators.DecidesProposal()
	proposal := message.NewVote(message.ProposalType, height, round, &data)
	c.validators.CustomValidators.BroadCast(proposal)
}

func (c *Core) enterPrevote(height int64, round int) {
	if c.Height != height || round < c.Round || (c.Round == round && RoundStepPrevote <= c.Step) {
		log.Debug(fmt.Sprintf("enterPrevote(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, c.Height, c.Round, c.Step))
		return
	}

	defer func() {
		// Done enterPrevote:
		c.updateRoundStep(round, RoundStepPrevote)
	}()

	log.Info(fmt.Sprintf("enterPrevote(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

	// Sign and broadcast vote as necessary
	c.doPrevote(height, round)

	// Once `addVote` hits any +2/3 prevotes, we will go to PrevoteWait
	// (so we have more time to try and collect +2/3 prevotes for a single block)
}

func (c *Core) doPrevote(height int64, round int) {
	// TODO:
}

func (c *Core) enterPrevoteWait(height int64, round int) {
	// TODO:
}

func (c *Core) enterPrecommit(height int64, round int) {
	// TODO:
}

func (c *Core) enterPrecommitWait(height int64, round int) {
	// TODO:
}

func (c *Core) enterCommit(height int64, round int) {
	// TODO:
}

// Attempt to add the vote. if its a duplicate signature, dupeout the validator
func (c *Core) tryAddVote(vote *message.Vote) (bool, error) {
	added, err := c.addVote(vote)
	if err != nil {
		// If the vote height is off, we'll just ignore it,
		// But if it's a conflicting sig, add it to the c.evpool.
		// If it's otherwise invalid, punish peer.
		if err == ErrVoteHeightMismatch {
			return added, err
		} else if _, ok := err.(*ErrVoteConflictingVotes); ok {
			// TODO:
			//if bytes.Equal(vote.ValidatorAddress, c.privValidator.GetAddress()) {
			//	log.Error("Found conflicting vote from ourselves. Did you unsafe_reset a validator?", "height", vote.Height, "round", vote.Round, "type", vote.Type)
			//	return added, err
			//}
			return added, err
		} else {
			// Probably an invalid signature / Bad peer.
			// Seems this can also err sometimes with "Unexpected step" - perhaps not from a bad peer ?
			log.Error("Error attempting to add vote", "err", err)
			return added, ErrAddingVote
		}
	}
	return added, nil
}

func (c *Core) addVote(vote *message.Vote) (added bool, err error) {
	log.Debug("addVote", "voteHeight", vote.Height, "voteType", vote.Type, "csHeight", c.Height)

	// A precommit for the previous height?
	// These come in while we wait timeoutCommit
	if vote.Height+1 == c.Height {
		if !(c.Step == RoundStepNewHeight && vote.Type == message.PrecommitType) {
			// TODO: give the reason ..
			// fmt.Errorf("tryAddVote: Wrong height, not a LastCommit straggler commit.")
			return added, ErrVoteHeightMismatch
		}
		added, err = c.LastCommit.AddVote(vote)
		if !added {
			return added, err
		}

		log.Info(fmt.Sprintf("Added to lastPrecommits: %v", c.LastCommit.String()))

		// if we can skip timeoutCommit and have all the votes now,
		if c.cfg.SkipTimeoutCommit && c.LastCommit.HasAll() {
			// go straight to new round (skip timeout commit)
			// c.scheduleTimeout(time.Duration(0), c.Height, 0, cstypes.RoundStepNewHeight)
			c.enterNewRound(c.Height, 0)
		}

		return
	}

	// Height mismatch is ignored.
	// Not necessarily a bad peer, but not favourable behaviour.
	if vote.Height != c.Height {
		err = ErrVoteHeightMismatch
		log.Info("Vote ignored and not added", "voteHeight", vote.Height, "csHeight", c.Height, "err", err)
		return
	}

	height := c.Height
	added, err = c.Votes.AddVote(vote)
	if !added {
		return
	}

	switch vote.Type {
	case message.ProposalType:
		// TODO:
	case message.PrevoteType:
		prevotes := c.Votes.Prevotes(vote.Round)
		log.Info("Added to prevote", "vote", vote, "prevotes", prevotes.String())

		// If +2/3 prevotes for a block or nil for *any* round:
		if propData, ok := prevotes.TwoThirdsMajority(); ok {

			// There was a polka!
			// If we're locked but this is a recent polka, unlock.
			// If it matches our ProposalBlock, update the ValidBlock

			// Unlock if `c.LockedRound < vote.Round <= c.Round`
			// NOTE: If vote.Round > c.Round, we'll deal with it when we get to vote.Round
			if (c.LockedProposal != nil) &&
				(c.LockedRound < vote.Round) &&
				(vote.Round <= c.Round) &&
				c.LockedProposal.Proposed != propData {

				log.Info("Unlocking because of POL.", "lockedRound", c.LockedRound, "POLRound", vote.Round)
				c.LockedRound = -1
				c.LockedProposal = nil
			}

			// NOTE: our proposal may be nil or not what received a polka..
			if propData != message.NilData && (vote.Round == c.Round) {
				if c.Proposal != nil && c.Proposal.Proposed != propData {
					log.Info(
						"Polka. Valid ProposedData we don't know about. Set Proposal=nil",
						"expect proposal:", c.Proposal.Proposed, "polka proposal", propData)
					// We're getting the wrong proposal.
					c.Proposal = nil
				}
			}
		}

		// If +2/3 prevotes for *anything* for future round:
		if c.Round < vote.Round && prevotes.HasTwoThirdsAny() {
			// Round-skip if there is any 2/3+ of votes ahead of us
			c.enterNewRound(height, vote.Round)
		} else if c.Round == vote.Round && RoundStepPrevote <= c.Step { // current round
			data, ok := prevotes.TwoThirdsMajority()
			if ok {
				if c.Proposal != nil || data == message.NilData {
					c.enterPrecommit(height, vote.Round)
				} else {
					log.Errorf("received polka on %v, but we didn't get the right proposal. height: %d, round: %d", data, c.Height, c.Round)
				}
			} else if prevotes.HasTwoThirdsAny() {
				c.enterPrevoteWait(height, vote.Round)
			}
		} else if RoundStepPrevote > c.Step {
			// If the proposal is now complete, enter prevote of c.Round.
			if c.Proposal != nil {
				c.enterPrevote(height, c.Round)
			} else {
				log.Error("receive prevote for ProposedData (%v), but we don't have proposal", vote.Proposed)
			}
		}

	case message.PrecommitType:
		precommits := c.Votes.Precommits(vote.Round)
		log.Info("Added to precommit", "vote", vote, "precommits", precommits.String())

		data, ok := precommits.TwoThirdsMajority()
		if ok {
			// Executed as TwoThirdsMajority could be from a higher round
			c.enterNewRound(height, vote.Round)
			c.enterPrecommit(height, vote.Round)
			if data != message.NilData {
				c.enterCommit(height, vote.Round)
				if c.cfg.SkipTimeoutCommit && precommits.HasAll() {
					c.enterNewRound(c.Height, 0)
				}
			} else {
				c.enterPrecommitWait(height, vote.Round)
			}
		} else if c.Round <= vote.Round && precommits.HasTwoThirdsAny() {
			c.enterNewRound(height, vote.Round)
			c.enterPrecommitWait(height, vote.Round)
		}

	default:
		panic(fmt.Sprintf("Unexpected vote type %X", vote.Type)) // go-wire should prevent this.
	}

	return
}

// Attempt to schedule a timeout (by sending timeoutInfo on the tickChan)
func (c *Core) scheduleTimeout(duration time.Duration, height int64, round int, step RoundStepType) {
	c.timeoutTicker.ScheduleTimeout(timeoutInfo{duration, height, round, step})
}
