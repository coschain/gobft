package gobft

import (
	"fmt"
	"reflect"
	"runtime/debug"
	"sync"
	"time"

	"github.com/coschain/gobft/common"
	"github.com/coschain/gobft/custom"
	"github.com/coschain/gobft/message"
	"github.com/sirupsen/logrus"
)

type Core struct {
	name       string
	cfg        *Config
	validators *Validators

	RoundState
	triggeredTimeoutPrecommit bool

	msgQueue      chan msgInfo
	timeoutTicker TimeoutTicker
	done          chan struct{}

	log *logrus.Entry

	sync.RWMutex

	// for test only
	byzantinePrevote *message.ProposedData
}

func NewCore(vals custom.ICommittee, pVal custom.IPrivValidator) *Core {
	c := &Core{
		cfg:        DefaultConfig(),
		validators: NewValidators(vals, pVal),
		msgQueue:   make(chan msgInfo, msgQueueSize),
		//timeoutTicker: NewTimeoutTicker(),
		done: make(chan struct{}),
	}
	c.log = logrus.WithField("CoreName", c.name)
	c.timeoutTicker = NewTimeoutTicker(c)
	logrus.SetLevel(logrus.InfoLevel)

	return c
}

func (c *Core) SetName(n string) {
	c.name = n
	c.log = logrus.WithField("CoreName", c.name)
}

func (c *Core) Start() error {
	if err := c.timeoutTicker.Start(); err != nil {
		return err
	}

	appState := c.validators.CustomValidators.GetAppState()
	c.updateToAppState(appState)

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

func (c *Core) GetLastCommit() *message.Commit {
	c.RLock()
	defer c.RUnlock()

	return c.LastCommit.MakeCommit()
}

// RecvMsg accepts a ConsensusMessage and delivers it to receiveRoutine
func (c *Core) RecvMsg(msg message.ConsensusMessage) {
	if err := msg.ValidateBasic(); err != nil {
		c.log.Error(err)
		return
	}
	c.sendInternalMessage(msgInfo{msg})

}

// enterNewRound(height, 0) at c.StartTime.
func (c *Core) scheduleRound0(rs *RoundState) {
	c.log.Info("scheduleRound0", " now ", common.Now(), " startTime ", c.StartTime)
	sleepDuration := rs.StartTime.Sub(common.Now()) // nolint: gotype, gosimple
	c.scheduleTimeout(sleepDuration, rs.Height, 0, RoundStepNewHeight)
}

func (c *Core) updateRoundStep(round int, step RoundStepType) {
	c.Round = round
	c.Step = step
}

func (c *Core) updateToAppState(appState *message.AppState) {
	if appState == nil {
		return
	}

	if c.CommitRound > -1 && 0 < c.Height && c.Height != appState.LastHeight {
		common.PanicSanity(fmt.Sprintf("updateToState() expected state height of %v but found %v",
			c.Height, appState.LastHeight))
	}

	var lastPrecommits *VoteSet
	if c.CommitRound > -1 && c.Votes != nil {
		if !c.Votes.Precommits(c.CommitRound).HasTwoThirdsMajority() {
			common.PanicSanity("updateToState(state) called but last Precommit round didn't have +2/3")
		}
		lastPrecommits = c.Votes.Precommits(c.CommitRound)
	}

	// Next desired bft height
	c.Height = appState.LastHeight + 1
	c.updateRoundStep(0, RoundStepNewHeight)
	if c.CommitTime.IsZero() {
		// "Now" makes it easier to sync up dev nodes.
		// We add timeoutCommit to allow transactions
		// to be gathered for the first block.
		// And alternative solution that relies on clocks:
		//  c.StartTime = state.LastBlockTime.Add(timeoutCommit)
		c.StartTime = c.cfg.Commit(common.Now())
	} else {
		c.StartTime = c.cfg.Commit(c.CommitTime)
	}

	c.Proposal = nil
	c.LockedRound = -1
	c.LockedProposal = nil

	c.Votes = NewHeightVoteSet(c.Height, c.validators)
	c.CommitRound = -1
	c.LastCommit = lastPrecommits
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
			c.log.Error("CONSENSUS FAILURE!!!", " err ", r, " stack ", string(debug.Stack()))
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

	c.log.Debug("handleMsg: ", mi.Msg)
	var err error
	msg := mi.Msg
	if err = msg.ValidateBasic(); err != nil {
		c.log.Error(err)
		return
	}

	switch msg := msg.(type) {
	case *message.Vote:
		// attempt to add the vote and dupeout the validator if its a duplicate signature
		// if the vote gives us a 2/3-any or 2/3-one, we transition
		//var added bool
		_, err = c.tryAddVote(msg)

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
	case *message.Commit:
	default:
		c.log.Error("Unknown msg type ", reflect.TypeOf(msg))
	}
	if err != nil {
		c.log.Error("Error with msg ", " height ", c.Height, " round ", c.Round, " type ", reflect.TypeOf(msg), " err ", err, " msg ", msg)
	}
}

func (c *Core) handleTimeout(ti timeoutInfo, rs RoundState) {
	c.log.Debug("Received tock ", " timeout ", ti.Duration, " height ", ti.Height, " round ", ti.Round, " step ", ti.Step)

	// timeouts must be for current height, round, step
	if ti.Height != rs.Height || ti.Round < rs.Round || (ti.Round == rs.Round && ti.Step < rs.Step) {
		c.log.Debug("Ignoring tock because we're ahead ", " height ", rs.Height, " round ", rs.Round, " step ", rs.Step)
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
		c.log.Debug(fmt.Sprintf("enterNewRound(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, c.Height, c.Round, c.Step))
		return
	}

	if now := common.Now(); c.StartTime.After(now) {
		c.log.Info("Need to set a buffer and c.log message here for sanity.", "startTime", c.StartTime, "now", now)
	}

	c.log.Info(fmt.Sprintf("enterNewRound(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

	// Setup new round
	// we don't fire newStep for this step,
	// but we fire an event, so update the round step first
	c.updateRoundStep(round, RoundStepNewRound)
	if round == 0 {
		// We've already reset these upon new height,
		// and meanwhile we might have received a proposal
		// for round 0.
	} else {
		c.log.Infof("Resetting Proposal info, height %d, round %d", height, round)
		c.Proposal = nil
	}
	c.Votes.SetRound(round + 1) // also track next round (round+1) to allow round-skipping
	c.triggeredTimeoutPrecommit = false

	c.enterPropose(height, round)
}

func (c *Core) isReadyToPrevote() bool {
	// TODO:
	if c.Proposal != nil || c.LockedRound >= 0 {
		return true
	}

	return false
}

func (c *Core) isValidator() bool {
	self := c.validators.GetSelfPubKey()
	return c.validators.CustomValidators.IsValidator(self)
}

// Enter (CreateEmptyBlocks): from enterNewRound(height,round)
// Enter (CreateEmptyBlocks, CreateEmptyBlocksInterval > 0 ): after enterNewRound(height,round), after timeout of CreateEmptyBlocksInterval
// Enter (!CreateEmptyBlocks) : after enterNewRound(height,round), once txs are in the mempool
func (c *Core) enterPropose(height int64, round int) {
	if c.Height != height || round < c.Round || (c.Round == round && RoundStepPropose <= c.Step) {
		c.log.Debug(fmt.Sprintf("enterPropose(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, c.Height, c.Round, c.Step))
		return
	}
	c.log.Info(fmt.Sprintf("enterPropose(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

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
		c.log.Debug("This node is not a validator")
		return
	}

	if c.validators.CustomValidators.GetCurrentProposer(c.Round) == self {
		c.log.Info("enterPropose: Our turn to propose", " proposer ", self)
		c.doPropose(height, round)
	} else {
		c.log.Debug("enterPropose: Not our turn to propose", " proposer ",
			c.validators.CustomValidators.GetCurrentProposer(c.Round), " self ", self)
	}
}

func (c *Core) doPropose(height int64, round int) {
	data := c.validators.CustomValidators.DecidesProposal()
	proposal := message.NewVote(message.ProposalType, height, round, &data)

	if c.LockedRound > -1 && c.LockedProposal != nil {
		proposal.Proposed = c.LockedProposal.Proposed
	}

	c.signAddVote(proposal)
	c.Proposal = proposal
}

func (c *Core) enterPrevote(height int64, round int) {
	if c.Height != height || round < c.Round || (c.Round == round && RoundStepPrevote <= c.Step) {
		c.log.Debug(fmt.Sprintf("enterPrevote(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, c.Height, c.Round, c.Step))
		return
	}

	defer func() {
		// Done enterPrevote:
		c.updateRoundStep(round, RoundStepPrevote)
	}()

	c.log.Info(fmt.Sprintf("enterPrevote(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

	// Sign and broadcast vote as necessary
	c.doPrevote(height, round)

	// Once `addVote` hits any +2/3 prevotes, we will go to PrevoteWait
	// (so we have more time to try and collect +2/3 prevotes for a single block)
}

// sign the vote, publish on internalMsgQueue and broadcast
func (c *Core) signAddVote(vote *message.Vote) {
	// if we're not a validator, do nothing
	if !c.isValidator() {
		return
	}
	c.validators.Sign(vote)
	c.sendInternalMessage(msgInfo{vote})
	c.validators.CustomValidators.BroadCast(vote)
}

func (c *Core) sendInternalMessage(mi msgInfo) {
	select {
	case c.msgQueue <- mi:
		c.log.Debugf("recv %v", mi.Msg)
	default:
		// NOTE: using the go-routine means our votes can
		// be processed out of order.
		// TODO: use CList here for strict determinism and
		// attempt push to internalMsgQueue in receiveRoutine
		c.log.Info("Internal msg queue is full. Using a go-routine")
		go func() { c.msgQueue <- mi }()
	}
}

func (c *Core) setByzantinePrevote(data *message.ProposedData) {
	c.byzantinePrevote = data
}

func (c *Core) doPrevote(height int64, round int) {
	var prevote *message.Vote

	if c.byzantinePrevote != nil && *c.byzantinePrevote != message.NilData {
		prevote = message.NewVote(message.PrevoteType, c.Height, c.Round, c.byzantinePrevote)
		c.signAddVote(prevote)
		return
	}

	if c.LockedRound >= 0 && c.LockedProposal != nil {
		c.log.Info("enterPrevote: vote for POLed proposal: ", c.LockedProposal.Proposed)
		prevote = message.NewVote(message.PrevoteType, c.Height, c.Round, &c.LockedProposal.Proposed)
	} else if c.Proposal != nil &&
		c.Proposal.Proposed == c.validators.CustomValidators.DecidesProposal() {
		prevote = message.NewVote(message.PrevoteType, c.Height, c.Round, &c.Proposal.Proposed)
	} else {
		c.log.Info("enterPrevote: vote for nil")
		prevote = message.NewVote(message.PrevoteType, c.Height, c.Round, &message.NilData)
	}

	c.signAddVote(prevote)
}

func (c *Core) enterPrevoteWait(height int64, round int) {
	if c.Height != height || round < c.Round || (c.Round == round && RoundStepPrevoteWait <= c.Step) {
		c.log.Debug(fmt.Sprintf("enterPrevoteWait(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, c.Height, c.Round, c.Step))
		return
	}
	if !c.Votes.Prevotes(round).HasTwoThirdsAny() {
		common.PanicSanity(fmt.Sprintf("enterPrevoteWait(%v/%v), but Prevotes does not have any +2/3 votes", height, round))
	}
	c.log.Info(fmt.Sprintf("enterPrevoteWait(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

	defer func() {
		// Done enterPrevoteWait:
		c.updateRoundStep(round, RoundStepPrevoteWait)
	}()

	// Wait for some more prevotes; enterPrecommit
	c.scheduleTimeout(c.cfg.Prevote(round), height, round, RoundStepPrevoteWait)
}

func (c *Core) enterPrecommit(height int64, round int) {
	if c.Height != height || round < c.Round || (c.Round == round && RoundStepPrecommit <= c.Step) {
		c.log.Debug(fmt.Sprintf("enterPrecommit(%v/%v): Invalid args. Current step: %v/%v/%v", height, round, c.Height, c.Round, c.Step))
		return
	}

	c.log.Info(fmt.Sprintf("enterPrecommit(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

	defer func() {
		// Done enterPrecommit:
		c.updateRoundStep(round, RoundStepPrecommit)
	}()

	// check for a polkaData
	polkaData, ok := c.Votes.Prevotes(round).TwoThirdsMajority()

	precommit := message.NewVote(message.PrecommitType, height, round, &message.NilData)
	// If we don't have a polkaData, we must precommit nil.
	if !ok {
		if c.LockedProposal != nil {
			c.log.Info("enterPrecommit: No +2/3 prevotes during enterPrecommit while we're locked. Precommitting nil")
		} else {
			c.log.Info("enterPrecommit: No +2/3 prevotes during enterPrecommit. Precommitting nil.")
		}
		c.signAddVote(precommit)
		return
	}

	// the latest POLRound should be this round.
	polRound, _ := c.Votes.POLInfo()
	if polRound != round {
		common.PanicSanity(fmt.Sprintf("This POLRound should be %v but got %v", round, polRound))
	}

	// +2/3 prevoted nil. Unlock and precommit nil.
	if polkaData == message.NilData {
		if c.LockedProposal == nil {
			c.log.Info("enterPrecommit: +2/3 prevoted for nil.")
		} else {
			c.log.Info("enterPrecommit: +2/3 prevoted for nil. Unlocking")
			c.LockedRound = -1
			c.LockedProposal = nil
		}
		c.signAddVote(precommit)
		return
	}

	// At this point, +2/3 prevoted for a particular proposal.

	// If we're already locked on that proposed data, precommit it, and update the LockedRound
	if c.LockedRound >= 0 && c.LockedProposal.Proposed == polkaData {
		c.log.Info("enterPrecommit: +2/3 prevoted locked block. Relocking")
		c.LockedRound = round
		precommit.Proposed = polkaData
		c.signAddVote(precommit)
		return
	}

	// If +2/3 prevoted for proposal block, stage and precommit it
	if c.Proposal != nil && c.Proposal.Proposed == polkaData {
		c.log.Info("enterPrecommit: +2/3 prevoted proposal block. Locking", " proposed ", polkaData)
		c.LockedRound = round
		c.LockedProposal = c.Proposal
		precommit.Proposed = polkaData
		c.signAddVote(precommit)
		return
	}

	// If we get here， it means:
	// 1. our LockedProposal doesn't match the polka(this should never happen cuz once we got polka,
	//    lock on different proposed data is released)
	// 2. we have a polka on some proposed data that we don't have
	if c.LockedRound >= 0 && c.LockedProposal.Proposed != polkaData {
		common.PanicSanity(fmt.Sprintf(
			"[enterPrecommit] we are locked on %v but receive polka on %v",
			c.LockedProposal.Proposed, polkaData))
	}

	c.log.Errorf("Got a polkaData %v but we don't have its proposal", polkaData)
	c.LockedRound = -1
	c.LockedProposal = nil

	c.signAddVote(precommit)
}

func (c *Core) enterPrecommitWait(height int64, round int) {
	if c.Height != height || round < c.Round || (c.Round == round && c.triggeredTimeoutPrecommit) {
		c.log.Debug(
			fmt.Sprintf(
				"enterPrecommitWait(%v/%v): Invalid args. "+
					"Current state is Height/Round: %v/%v/, triggeredTimeoutPrecommit:%v",
				height, round, c.Height, c.Round, c.triggeredTimeoutPrecommit))
		return
	}
	if !c.Votes.Precommits(round).HasTwoThirdsAny() {
		common.PanicSanity(fmt.Sprintf("enterPrecommitWait(%v/%v), but Precommits does not have any +2/3 votes", height, round))
	}
	c.log.Info(fmt.Sprintf("enterPrecommitWait(%v/%v). Current: %v/%v/%v", height, round, c.Height, c.Round, c.Step))

	defer func() {
		// Done enterPrecommitWait:
		c.triggeredTimeoutPrecommit = true
	}()

	// Wait for some more precommits; enterNewRound
	c.scheduleTimeout(c.cfg.Precommit(round), height, round, RoundStepPrecommitWait)
}

func (c *Core) enterCommit(height int64, commitRound int) {
	if c.Height != height || RoundStepCommit <= c.Step {
		c.log.Debug(fmt.Sprintf("enterCommit(%v/%v): Invalid args. Current step: %v/%v/%v", height, commitRound, c.Height, c.Round, c.Step))
		return
	}
	c.log.Info(fmt.Sprintf("enterCommit(%v/%v). Current: %v/%v/%v", height, commitRound, c.Height, c.Round, c.Step))

	maj23, ok := c.Votes.Precommits(commitRound).TwoThirdsMajority()
	if !ok {
		common.PanicSanity("RunActionCommit() expects +2/3 precommits")
	}

	c.CommitRound = commitRound
	c.CommitTime = common.Now()

	c.updateRoundStep(c.Round, RoundStepCommit)
	c.doCommit(maj23)
}

func (c *Core) doCommit(data message.ProposedData) {
	// if we're the current proposer, generate a Commit msg so that
	// the app can store and broadcast it
	self := c.validators.GetSelfPubKey()
	records := (*message.Commit)(nil)
	if c.validators.CustomValidators.GetCurrentProposer(c.CommitRound) == self {
		records = c.Votes.Precommits(c.CommitRound).MakeCommit()
		records.CommitTime = c.CommitTime
		c.validators.Sign(records)
	}

	c.validators.CustomValidators.Commit(data, records)

	appState := c.validators.CustomValidators.GetAppState()
	c.updateToAppState(appState)

	// c.StartTime is already set.
	// Schedule Round0 to start soon.
	c.scheduleRound0(&c.RoundState)
}

// Attempt to add the vote. if its a duplicate signature, dupeout the validator
func (c *Core) tryAddVote(vote *message.Vote) (bool, error) {
	added, err := c.addVote(vote)
	if err != nil {
		c.log.Error("---addVote ", err)
		// If the vote height is off, we'll just ignore it,
		// But if it's a conflicting sig, add it to the c.evpool.
		// If it's otherwise invalid, punish peer.
		if err == ErrVoteHeightMismatch {
			return added, err
		} else if _, ok := err.(*ErrVoteConflictingVotes); ok {
			// TODO: catch conflict votes
			return added, err
		} else {
			// Probably an invalid signature / Bad peer.
			// Seems this can also err sometimes with "Unexpected step" - perhaps not from a bad peer ?
			c.log.Error("Error attempting to add vote", " err ", err)
			return added, ErrAddingVote
		}
	}
	return added, nil
}

func (c *Core) addVote(vote *message.Vote) (added bool, err error) {
	c.log.Debug("addVote ", " voteHeight ", vote.Height, " voteType ", vote.Type, " cHeight ", c.Height)

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

		c.log.Info(fmt.Sprintf("Added to lastPrecommits: %v", c.LastCommit.String()))

		// if we can skip timeoutCommit and have all the votes now,
		if c.cfg.SkipTimeoutCommit && c.LastCommit.HasAll() {
			// go straight to new round (skip timeout commit)
			// c.scheduleTimeout(time.Duration(0), c.Height, 0, ctypes.RoundStepNewHeight)
			c.enterNewRound(c.Height, 0)
		}

		return
	}

	// Height mismatch is ignored.
	// Not necessarily a bad peer, but not favourable behaviour.
	if vote.Height != c.Height {
		err = ErrVoteHeightMismatch
		c.log.Info("Vote ignored and not added", "voteHeight", vote.Height, "cHeight", c.Height, "err", err)
		return
	}

	if vote.Type == message.ProposalType {
		c.log.Debug("defaultSetProposal")
		err = c.defaultSetProposal(vote)
		return
	}

	height := c.Height
	added, err = c.Votes.AddVote(vote)
	if !added {
		return
	}

	switch vote.Type {
	case message.PrevoteType:
		prevotes := c.Votes.Prevotes(vote.Round)
		c.log.Debug("Added to prevote", " vote ", vote, " prevotes ", prevotes.String())

		// If +2/3 prevotes for a block or nil for *any* round:
		if polkaData, ok := prevotes.TwoThirdsMajority(); ok {
			c.log.Info("POLKA!!! ", prevotes.String())

			// There was a polkaData!
			// If we're locked but this is a recent polkaData, unlock.
			// If it matches our ProposalBlock, update the ValidBlock

			// Unlock if `c.LockedRound < vote.Round <= c.Round`
			// NOTE: If vote.Round > c.Round, we'll deal with it when we get to vote.Round
			if (c.LockedProposal != nil) &&
				(c.LockedRound < vote.Round) &&
				(vote.Round <= c.Round) &&
				c.LockedProposal.Proposed != polkaData {

				c.log.Info("Unlocking because of POL.", " lockedRound ", c.LockedRound, " POLRound ", vote.Round)
				c.LockedRound = -1
				c.LockedProposal = nil
			}

			// NOTE: our proposal may be nil or not what received a polkaData..
			if polkaData != message.NilData && (vote.Round == c.Round) {
				if c.Proposal != nil && c.Proposal.Proposed != polkaData {
					c.log.Warn(
						"Polka. Valid ProposedData we don't know about. Set Proposal=nil",
						"expect proposal:", c.Proposal.Proposed, " polkaData proposal ", polkaData)
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
			polkaData, ok := prevotes.TwoThirdsMajority()
			if ok {
				// c.Proposal != nil means we got polka on it, cuz if we a polka on something else
				// other than the Proposal, it'll be set to nil
				// if we got polka on NilData, then Proposal will be set to nil cuz no one should
				// propose NilData
				if c.Proposal != nil || polkaData == message.NilData {
					c.enterPrecommit(height, vote.Round)
				} else {
					c.log.Errorf("received polkaData %v, but we didn't get the right proposal. height: %d, round: %d", polkaData, c.Height, c.Round)
				}
			} else if prevotes.HasTwoThirdsAny() {
				c.enterPrevoteWait(height, vote.Round)
			}
		} else if RoundStepPrevote > c.Step {
			// If the proposal is now complete, enter prevote of c.Round.
			if c.Proposal != nil {
				c.enterPrevote(height, c.Round)
			} else {
				c.log.Errorf("receive prevote for ProposedData (%v), but we don't have proposal", vote.Proposed)
			}
		}

	case message.PrecommitType:
		precommits := c.Votes.Precommits(vote.Round)
		c.log.Debug("Added to precommit", " vote ", vote, " precommits ", precommits.String())

		maj23, ok := precommits.TwoThirdsMajority()
		if ok {
			// Executed as TwoThirdsMajority could be from a higher round
			c.enterNewRound(height, vote.Round)
			c.enterPrecommit(height, vote.Round)
			if maj23 != message.NilData {
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

func (c *Core) defaultSetProposal(proposal *message.Vote) error {
	// Already have one
	if c.Proposal != nil {
		c.log.Debugf("Already got proposal %v from %s, get another proposal %v from %s",
			c.Proposal.Proposed, c.Proposal.Address, proposal.Proposed, proposal.Address)
		return nil
	}

	// Does not apply
	if proposal.Height != c.Height || proposal.Round != c.Round {
		c.log.Error("mismatch ", proposal)
		return nil
	}

	// check if proposal is from the current proposer
	if c.validators.CustomValidators.GetCurrentProposer(c.Round) != proposal.Address {
		c.log.Errorf("invalid proposer. want %v, got %v",
			c.validators.CustomValidators.GetCurrentProposer(c.Round), proposal.Address)
		return ErrInvalidProposer
	}

	// Verify signature
	if !c.validators.VerifySignature(proposal) {
		c.log.Error("invalid sig ", proposal)
		return ErrInvalidProposalSignature
	}

	// Only accept the proposal and set Core.Proposal when CustomValidators approves it
	if c.validators.CustomValidators.ValidateProposal(proposal.Proposed) {
		c.Proposal = proposal
		c.log.Debug("Accept proposal", " proposal ", proposal)
	} else {
		c.log.Warnf("invalid proposal, want %v got %v",
			c.validators.CustomValidators.DecidesProposal(), proposal.Proposed)
	}
	return nil
}

// Attempt to schedule a timeout (by sending timeoutInfo on the tickChan)
func (c *Core) scheduleTimeout(duration time.Duration, height int64, round int, step RoundStepType) {
	c.timeoutTicker.ScheduleTimeout(timeoutInfo{duration, height, round, step})
}
