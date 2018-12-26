package gobft

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"testing"

	"github.com/coschain/gobft/custom"
	"github.com/coschain/gobft/message"
	"github.com/golang/mock/gomock"
	"github.com/sirupsen/logrus"
)

const nodeNum = 1

func TestBFT(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// init IPubValidator and IPrivValidator
	var pubKeys [nodeNum]message.PubKey
	var pubVals [nodeNum]*custom.MockIPubValidator
	for j := 0; j < nodeNum; j++ {
		i := j
		pubKeys[i] = message.PubKey("val_pubkey" + strconv.Itoa(i))
		pubVals[i] = custom.NewMockIPubValidator(ctrl)
		pubVals[i].EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
		pubVals[i].EXPECT().VerifySig(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
		pubVals[i].EXPECT().GetPubKey().Return(pubKeys[i]).AnyTimes()
	}

	var privVals [nodeNum]*custom.MockIPrivValidator
	for j := 0; j < nodeNum; j++ {
		i := j
		privVals[i] = custom.NewMockIPrivValidator(ctrl)
		privVals[i].EXPECT().GetPubKey().Return(pubKeys[i]).AnyTimes()
		privVals[i].EXPECT().Sign(gomock.Any()).DoAndReturn(func(digest []byte) []byte {
			return digest
		}).AnyTimes()
	}

	// init committee
	stopCh := make(chan struct{})
	commitTimes := 0
	initState := &message.AppState{
		LastHeight:       0,
		LastProposedData: message.NilData,
	}
	var committedStates [4][]*message.AppState
	curProposers := make([]*custom.MockIPubValidator, 0)
	for i := 0; i < nodeNum; i++ {
		curProposers = append(curProposers, pubVals[i])
	}
	var indeces [nodeNum]int
	var proposedData message.ProposedData = sha256.Sum256([]byte("hello"))
	var committees [nodeNum]*custom.MockICommittee
	for j := 0; j < nodeNum; j++ {
		i := j
		committedStates[i] = append(committedStates[i], initState)
		indeces[i] = 0

		committees[i] = custom.NewMockICommittee(ctrl)
		committees[i].EXPECT().GetValidator(pubKeys[i]).Return(pubVals[i]).AnyTimes()
		committees[i].EXPECT().IsValidator(gomock.Any()).Return(true).AnyTimes()
		committees[i].EXPECT().TotalVotingPower().Return(int64(nodeNum)).AnyTimes()
		committees[i].EXPECT().GetCurrentProposer().DoAndReturn(func() message.PubKey {
			return curProposers[indeces[i]].GetPubKey()
		}).AnyTimes()
		committees[i].EXPECT().DecidesProposal().Return(proposedData).AnyTimes()
		committees[i].EXPECT().Commit(gomock.Any()).DoAndReturn(func(data message.ProposedData) error {
			s := &message.AppState{
				LastHeight:       committedStates[i][len(committedStates[i])-1].LastHeight + 1,
				LastProposedData: data,
			}
			committedStates[i] = append(committedStates[i], s)
			logrus.Infof("core %d committed %v at height %d", i, data, s.LastHeight)
			commitTimes++
			if commitTimes == 4 {
				close(stopCh)
			}
			indeces[i] = (indeces[i] + 1) % nodeNum
			return nil
		}).AnyTimes()
		//committees[i].EXPECT().GetAppState().Return(committedStates[i][len(committedStates[i])-1]).AnyTimes()
		committees[i].EXPECT().GetAppState().DoAndReturn(func() *message.AppState {
			ret := committedStates[i][len(committedStates[i])-1]
			return ret
		}).AnyTimes()
	}

	// init bft core
	var cores [nodeNum]*Core
	for i := 0; i < nodeNum; i++ {
		cores[i] = NewCore(committees[i], privVals[i])
		cores[i].SetName("core" + strconv.Itoa(i))
	}
	for i := 0; i < nodeNum; i++ {
		ii := i
		cores[ii].validators.CustomValidators.(*custom.MockICommittee).EXPECT().
			BroadCast(gomock.Any()).DoAndReturn(func(msg message.ConsensusMessage) error {

			for j := 0; j < nodeNum; j++ {
				if ii != j {
					fmt.Printf("core %d broadcast %v to core%d\n", ii, msg, j)
					cores[j].RecvMsg(msg)
				}
			}
			return nil
		}).AnyTimes()
	}

	// start
	for i := 0; i < nodeNum; i++ {
		cores[i].Start()
	}

	<-stopCh
}
