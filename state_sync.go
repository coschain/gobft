package gobft

import "github.com/coschain/gobft/message"

type StateSync struct {
	heightVotes map[int64]*HeightVoteSet
	core        *Core
}

func NewStateSync(c *Core) *StateSync {
	return &StateSync{
		heightVotes: make(map[int64]*HeightVoteSet),
		core:        c,
	}
}

func (s *StateSync) AddVote(v *message.Vote) {
	s.core.log.Info("[StateSync] AddVote ", v.String())

	for k := range s.heightVotes {
		if k <= s.core.Height {
			delete(s.heightVotes, k)
		}
	}

	votesByHeight, ok := s.heightVotes[v.Height]
	if !ok {
		votesByHeight = NewHeightVoteSet(v.Height, s.core.validators)
		s.heightVotes[v.Height] = votesByHeight
	}

	added, err := votesByHeight.AddVote(v)
	if !added {
		if err != nil {
			s.core.log.Error("StateSync AddVote: ", err)
		}
		return
	}

	if v.Type == message.PrevoteType {
		prevotes := votesByHeight.Prevotes(v.Round)
		if prevotes.HasTwoThirdsMajority() {
			s.updateState(v.Height, v.Round, RoundStepNewHeight)
			s.core.enterNewRound(v.Height, v.Round)
			s.core.enterPrecommit(v.Height, v.Round)
		} else if prevotes.HasTwoThirdsAny() {
			s.updateState(v.Height, v.Round, RoundStepNewHeight)
			s.core.enterNewRound(v.Height, v.Round)
			//s.core.enterPrevote(v.Height, v.Round)
			s.core.enterPrevoteWait(v.Height, v.Round)
		}
	} else if v.Type == message.PrecommitType {
		precommits := votesByHeight.Precommits(v.Round)
		if precommits.HasTwoThirdsMajority() {
			s.updateState(v.Height, v.Round, RoundStepNewHeight)
			s.core.enterNewRound(v.Height, v.Round)
			//s.core.enterPrecommit(v.Height, v.Round)
			s.core.enterCommit(v.Height, v.Round)
			if s.core.cfg.SkipTimeoutCommit && precommits.HasAll() {
				s.core.enterNewRound(v.Height, 0)
			}
		} else if precommits.HasTwoThirdsAny() {
			s.updateState(v.Height, v.Round, RoundStepNewHeight)
			s.core.enterNewRound(v.Height, v.Round)
			s.core.enterPrecommitWait(v.Height, v.Round)
		}
	}
}

func (s *StateSync) updateState(height int64, round int, step RoundStepType) {
	s.core.Height = height
	s.core.Round = round
	s.core.Step = step

	s.core.Votes = s.heightVotes[height]
	delete(s.heightVotes, height)
}
