package gobft

import (
	"crypto/sha256"
	"github.com/coschain/gobft/custom"
	"github.com/sirupsen/logrus"
	"strconv"
	"testing"

	"github.com/coschain/gobft/custom/mock"
	"github.com/coschain/gobft/message"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

const nodeNum = 4
const byzantineIdx = 2
const commitHeight = 5

var (
	pubKeys  [nodeNum]message.PubKey
	pubVals  [nodeNum]*mock.MockIPubValidator
	privVals [nodeNum]*mock.MockIPrivValidator
)

var (
	stopCh              [nodeNum]chan struct{}
	commitTimes         [nodeNum]int
	committedStates     [nodeNum][]*message.AppState
	curProposers        [nodeNum][]*mock.MockIPubValidator
	proposedData        message.ProposedData = sha256.Sum256([]byte("hello"))
	invalidProposedData message.ProposedData = sha256.Sum256([]byte("byzantine"))
	committees          [nodeNum]*mock.MockICommittee
)

func initValidators(ctrl *gomock.Controller) {
	for j := 0; j < nodeNum; j++ {
		i := j
		pubKeys[i] = message.PubKey("val_pubkey" + strconv.Itoa(i))
		pubVals[i] = mock.NewMockIPubValidator(ctrl)
		pubVals[i].EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
		pubVals[i].EXPECT().VerifySig(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
		pubVals[i].EXPECT().GetPubKey().Return(pubKeys[i]).AnyTimes()
	}

	for j := 0; j < nodeNum; j++ {
		i := j
		privVals[i] = mock.NewMockIPrivValidator(ctrl)
		privVals[i].EXPECT().GetPubKey().Return(pubKeys[i]).AnyTimes()
		privVals[i].EXPECT().Sign(gomock.Any()).DoAndReturn(func(digest []byte) []byte {
			return digest
		}).AnyTimes()
	}
}

func initCommittee(ctrl *gomock.Controller, byzantineIndex int, initState *message.AppState) {
	for j := 0; j < nodeNum; j++ {
		i := j

		for l := 0; l < nodeNum; l++ {
			curProposers[i] = append(curProposers[i], pubVals[l])
		}
		commitTimes[i] = 0
		committedStates[i] = append(committedStates[i], initState)

		committees[i] = mock.NewMockICommittee(ctrl)
		committees[i].EXPECT().GetValidator(gomock.Any()).DoAndReturn(func(pubKey message.PubKey) custom.IPubValidator {
			for k := 0; k < nodeNum; k++ {
				if pubKey == pubKeys[k] {
					return pubVals[k]
				}
			}
			return pubVals[0]
		}).AnyTimes()
		committees[i].EXPECT().IsValidator(gomock.Any()).Return(true).AnyTimes()
		committees[i].EXPECT().TotalVotingPower().Return(int64(nodeNum)).AnyTimes()
		committees[i].EXPECT().GetCurrentProposer(gomock.Any()).DoAndReturn(func(round int) message.PubKey {
			cur := curProposers[i][round%nodeNum]
			return cur.GetPubKey()
		}).AnyTimes()
		if i != byzantineIndex {
			committees[i].EXPECT().DecidesProposal().Return(proposedData).AnyTimes()
		}

		//committees[i].EXPECT().GetAppState().Return(committedStates[i][len(committedStates[i])-1]).AnyTimes()
		committees[i].EXPECT().GetAppState().DoAndReturn(func() *message.AppState {
			ret := committedStates[i][len(committedStates[i])-1]
			return ret
		}).AnyTimes()
	}
}

func TestBFT(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	assert := assert.New(t)

	// init IPubValidator and IPrivValidator
	initValidators(ctrl)

	// init committee
	for i := 0; i < nodeNum; i++ {
		stopCh[i] = make(chan struct{})
	}
	initState := &message.AppState{
		LastHeight:       0,
		LastProposedData: message.NilData,
	}
	initCommittee(ctrl, byzantineIdx, initState)

	// init bft core
	var cores [nodeNum]*Core
	for i := 0; i < nodeNum; i++ {
		cores[i] = NewCore(committees[i], privVals[i])
		cores[i].SetName("core" + strconv.Itoa(i))
	}

	// Init byzantine core. It always:
	// 1. propose invalidProposedData
	// 2. prevote invalidProposedData
	if nodeNum >= byzantineIdx {
		cores[byzantineIdx].validators.CustomValidators.(*mock.MockICommittee).EXPECT().
			DecidesProposal().Return(invalidProposedData).AnyTimes()
		cores[byzantineIdx].setByzantinePrevote(&invalidProposedData)
	}

	for i := 0; i < nodeNum; i++ {
		ii := i

		cores[ii].validators.CustomValidators.(*mock.MockICommittee).EXPECT().
			Commit(gomock.Any()).DoAndReturn(func(records *message.Commit) error {
			s := &message.AppState{
				LastHeight:       committedStates[ii][len(committedStates[ii])-1].LastHeight + 1,
				LastProposedData: records.ProposedData,
			}
			committedStates[ii] = append(committedStates[ii], s)
			logrus.Infof("core %d committed %v at height %d", ii, records.ProposedData, s.LastHeight)
			commitTimes[ii]++
			if commitTimes[ii] == commitHeight {
				close(stopCh[ii])
			}

			// shift proposer
			cur := curProposers[ii][0]
			for l := 1; l < nodeNum; l++ {
				curProposers[ii][l-1] = curProposers[ii][l]
			}
			curProposers[ii][nodeNum-1] = cur

			return nil
		}).AnyTimes()

		cores[ii].validators.CustomValidators.(*mock.MockICommittee).EXPECT().
			BroadCast(gomock.Any()).DoAndReturn(func(msg message.ConsensusMessage) error {
			bytes := msg.Bytes()
			ori, err := message.DecodeConsensusMsg(bytes)
			assert.Equal(err, nil)
			for j := 0; j < nodeNum; j++ {
				if ii != j {
					cores[j].RecvMsg(ori)
				}
			}
			return nil
		}).AnyTimes()
		cores[ii].validators.CustomValidators.(*mock.MockICommittee).EXPECT().
			ValidateProposal(gomock.Any()).DoAndReturn(func(data message.ProposedData) bool {
			return data == cores[ii].validators.CustomValidators.(*mock.MockICommittee).DecidesProposal()
		}).AnyTimes()
	}

	// start
	for i := 0; i < nodeNum; i++ {
		cores[i].Start()
	}

	for i := 0; i < nodeNum; i++ {
		<-stopCh[i]
	}

	for i := 0; i < nodeNum; i++ {
		assert.Equal(commitHeight+1, len(committedStates[i]))
	}
}

func TestStateSync(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	assert := assert.New(t)

	// init IPubValidator and IPrivValidator
	initValidators(ctrl)

	// init committee
	lastValStartCh := make(chan struct{})
	for i := 0; i < nodeNum; i++ {
		stopCh[i] = make(chan struct{})
	}
	initState := &message.AppState{
		LastHeight:       0,
		LastProposedData: message.NilData,
	}
	initCommittee(ctrl, -1, initState)

	// init bft core
	var cores [nodeNum]*Core
	for i := 0; i < nodeNum; i++ {
		cores[i] = NewCore(committees[i], privVals[i])
		cores[i].SetName("core" + strconv.Itoa(i))
	}

	for i := 0; i < nodeNum; i++ {
		ii := i

		cores[ii].validators.CustomValidators.(*mock.MockICommittee).EXPECT().
			Commit(gomock.Any()).DoAndReturn(func(records *message.Commit) error {
			s := &message.AppState{
				LastHeight:       committedStates[ii][len(committedStates[ii])-1].LastHeight + 1,
				LastProposedData: records.ProposedData,
			}
			committedStates[ii] = append(committedStates[ii], s)
			logrus.Infof("core %d committed %v at height %d", ii, records.ProposedData, s.LastHeight)
			commitTimes[ii]++
			if ii == 0 && commitTimes[ii] == 3 {
				close(lastValStartCh)
			} else if ii == nodeNum-1 && commitTimes[ii] == 2 {
				close(stopCh[ii])
				cores[ii].Stop()
			} else if commitTimes[ii] == commitHeight {
				close(stopCh[ii])
				cores[ii].Stop()
			}

			// shift proposer
			cur := curProposers[ii][0]
			for l := 1; l < nodeNum; l++ {
				curProposers[ii][l-1] = curProposers[ii][l]
			}
			curProposers[ii][nodeNum-1] = cur

			return nil
		}).AnyTimes()

		cores[ii].validators.CustomValidators.(*mock.MockICommittee).EXPECT().
			BroadCast(gomock.Any()).DoAndReturn(func(msg message.ConsensusMessage) error {
			//bytes := msg.Bytes()
			//ori, err := message.DecodeConsensusMsg(bytes)
			//assert.Equal(err, nil)
			if ii == nodeNum-1 {
				logrus.Info("mmmmmmmmmm = ", msg)
			}
			for j := 0; j < nodeNum; j++ {
				if ii != j {
					cores[j].RecvMsg(msg)
				}
			}
			return nil
		}).AnyTimes()
		cores[ii].validators.CustomValidators.(*mock.MockICommittee).EXPECT().
			ValidateProposal(gomock.Any()).DoAndReturn(func(data message.ProposedData) bool {
			return data == cores[ii].validators.CustomValidators.(*mock.MockICommittee).DecidesProposal()
		}).AnyTimes()
	}

	// start
	for i := 0; i < nodeNum-1; i++ {
		cores[i].Start()
	}

	<-lastValStartCh
	// since it's 3 commits behind,
	// 1. curProposers shift 3 times
	// 2. fix the committedStates
	for k := 0; k < 3; k++ {
		cur := curProposers[nodeNum-1][0]
		for l := 1; l < nodeNum; l++ {
			curProposers[nodeNum-1][l-1] = curProposers[nodeNum-1][l]
		}
		curProposers[nodeNum-1][nodeNum-1] = cur

		s := &message.AppState{
			LastHeight:       int64(k + 1),
			LastProposedData: proposedData,
		}
		committedStates[nodeNum-1] = append(committedStates[nodeNum-1], s)
	}
	cores[nodeNum-1].Start()

	for i := 0; i < nodeNum; i++ {
		<-stopCh[i]
	}

	for i := 0; i < nodeNum; i++ {
		assert.Equal(commitHeight+1, len(committedStates[i]))
	}
}
