package go_bft

import (
	"crypto/sha256"
	"testing"

	"github.com/coschain/go-bft/custom"
	"github.com/coschain/go-bft/message"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestVotes(t *testing.T) {
	//assert := assert.New(t)
	//NewValidators(1,)
	//voteSet := NewVoteSet(1, 0, message.PrevoteType)
}

func TestValidatorsVotes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// test IPubValidator and IPrivValidator
	var pubkey1 message.PubKey = "val1_pubkey"
	val1 := custom.NewMockIPubValidator(ctrl)
	val1.EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
	val1.EXPECT().VerifySig([]byte("digest1"), []byte("signature1")).Return(true).AnyTimes()
	val1.EXPECT().GetPubKey().Return(pubkey1).AnyTimes()

	var pubkey2 message.PubKey = "val2_pubkey"
	val2 := custom.NewMockIPubValidator(ctrl)
	val2.EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
	val2.EXPECT().VerifySig([]byte("digest2"), []byte("signature2")).Return(true).AnyTimes()
	val2.EXPECT().GetPubKey().Return(pubkey2).AnyTimes()

	var pubkey3 message.PubKey = "val3_pubkey"
	val3 := custom.NewMockIPubValidator(ctrl)
	val3.EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
	val3.EXPECT().VerifySig([]byte("digest3"), []byte("signature3")).Return(true).AnyTimes()
	val3.EXPECT().GetPubKey().Return(pubkey3).AnyTimes()

	var pubkey4 message.PubKey = "val4_pubkey"
	val4 := custom.NewMockIPubValidator(ctrl)
	val4.EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
	val4.EXPECT().VerifySig([]byte("digest4"), []byte("signature4")).Return(true).AnyTimes()
	val4.EXPECT().GetPubKey().Return(pubkey4).AnyTimes()

	privVal1 := custom.NewMockIPrivValidator(ctrl)
	privVal1.EXPECT().GetPubKey().Return(pubkey1).AnyTimes()
	privVal1.EXPECT().Sign([]byte("digest1")).Return([]byte("signature1")).AnyTimes()

	assert := assert.New(t)
	sig1 := privVal1.Sign([]byte("digest1"))
	assert.True(val1.VerifySig([]byte("digest1"), sig1))

	// init IValidators
	curProposers := []*custom.MockIPubValidator{val1, val2, val3, val4}
	idx := 0
	var proposedData message.ProposedData = sha256.Sum256([]byte("hello"))

	valSet := custom.NewMockIValidators(ctrl)
	valSet.EXPECT().GetValidator(pubkey1).Return(val1).AnyTimes()
	valSet.EXPECT().IsValidator(pubkey1).Return(true).AnyTimes()
	valSet.EXPECT().TotalVotingPower().Return(int64(4)).AnyTimes()
	valSet.EXPECT().GetCurrentProposer().DoAndReturn(func() *custom.MockIPubValidator {
		defer func() { idx = (idx + 1) % 4 }()
		return curProposers[idx]
	}).AnyTimes()
	valSet.EXPECT().DecidesProposal().Return(proposedData)
	assert.Equal(val1, valSet.GetCurrentProposer())
	assert.Equal(val2, valSet.GetCurrentProposer())
	assert.Equal(val3, valSet.GetCurrentProposer())
	assert.Equal(val4, valSet.GetCurrentProposer())
	assert.Equal(val1, valSet.GetCurrentProposer())
	assert.Equal(proposedData, valSet.DecidesProposal())

	//vs := NewValidators(valSet)
	//vs.UpdateValidators(1, valSet)
	//hvSet := NewHeightVoteSet(1, vs)
}
