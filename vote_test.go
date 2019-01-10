package gobft

import (
	"crypto/sha256"
	"testing"

	"github.com/coschain/gobft/custom/mock"
	"github.com/coschain/gobft/message"
	"github.com/golang/mock/gomock"
	"github.com/stretchr/testify/assert"
)

func TestValidatorsVotes(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	// test IPubValidator and IPrivValidator
	var pubkey1 message.PubKey = "val1_pubkey"
	val1 := mock.NewMockIPubValidator(ctrl)
	val1.EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
	val1.EXPECT().VerifySig([]byte("digest1"), []byte("signature1")).Return(true).AnyTimes()
	val1.EXPECT().GetPubKey().Return(pubkey1).AnyTimes()

	var pubkey2 message.PubKey = "val2_pubkey"
	val2 := mock.NewMockIPubValidator(ctrl)
	val2.EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
	val2.EXPECT().VerifySig([]byte("digest2"), []byte("signature2")).Return(true).AnyTimes()
	val2.EXPECT().GetPubKey().Return(pubkey2).AnyTimes()

	var pubkey3 message.PubKey = "val3_pubkey"
	val3 := mock.NewMockIPubValidator(ctrl)
	val3.EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
	val3.EXPECT().VerifySig([]byte("digest3"), []byte("signature3")).Return(true).AnyTimes()
	val3.EXPECT().GetPubKey().Return(pubkey3).AnyTimes()

	var pubkey4 message.PubKey = "val4_pubkey"
	val4 := mock.NewMockIPubValidator(ctrl)
	val4.EXPECT().GetVotingPower().Return(int64(1)).AnyTimes()
	val4.EXPECT().VerifySig([]byte("digest4"), []byte("signature4")).Return(true).AnyTimes()
	val4.EXPECT().GetPubKey().Return(pubkey4).AnyTimes()

	privVal1 := mock.NewMockIPrivValidator(ctrl)
	privVal1.EXPECT().GetPubKey().Return(pubkey1).AnyTimes()
	privVal1.EXPECT().Sign([]byte("digest1")).Return([]byte("signature1")).AnyTimes()
	privVal1.EXPECT().Sign(gomock.Any()).DoAndReturn(func(digest []byte) []byte {
		return digest
	}).AnyTimes()

	assert := assert.New(t)
	sig1 := privVal1.Sign([]byte("digest1"))
	assert.True(val1.VerifySig([]byte("digest1"), sig1))

	val1.EXPECT().VerifySig(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	val2.EXPECT().VerifySig(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	val3.EXPECT().VerifySig(gomock.Any(), gomock.Any()).Return(true).AnyTimes()
	val4.EXPECT().VerifySig(gomock.Any(), gomock.Any()).Return(true).AnyTimes()

	// init ICommittee
	curProposers := []*mock.MockIPubValidator{val1, val2, val3, val4}
	var proposedData message.ProposedData = sha256.Sum256([]byte("hello"))

	valSet := mock.NewMockICommittee(ctrl)
	valSet.EXPECT().GetValidator(pubkey1).Return(val1).AnyTimes()
	valSet.EXPECT().GetValidator(pubkey2).Return(val2).AnyTimes()
	valSet.EXPECT().GetValidator(pubkey3).Return(val3).AnyTimes()
	valSet.EXPECT().GetValidator(pubkey4).Return(val1).AnyTimes()
	valSet.EXPECT().IsValidator(gomock.Any()).Return(true).AnyTimes()
	valSet.EXPECT().TotalVotingPower().Return(int64(4)).AnyTimes()
	valSet.EXPECT().GetValidatorNum().Return(3).AnyTimes() // test minor quorum
	valSet.EXPECT().GetCurrentProposer(gomock.Any()).DoAndReturn(func(round int) message.PubKey {
		return curProposers[round%4].GetPubKey()
	}).AnyTimes()
	valSet.EXPECT().DecidesProposal().Return(proposedData)
	assert.Equal(val1.GetPubKey(), valSet.GetCurrentProposer(0))
	assert.Equal(val2.GetPubKey(), valSet.GetCurrentProposer(1))
	assert.Equal(val3.GetPubKey(), valSet.GetCurrentProposer(2))
	assert.Equal(val4.GetPubKey(), valSet.GetCurrentProposer(3))
	assert.Equal(val1.GetPubKey(), valSet.GetCurrentProposer(4))
	assert.Equal(proposedData, valSet.DecidesProposal())

	vs := NewValidators(valSet, privVal1)
	hvSet1 := NewHeightVoteSet(1, vs)

	// sign votes
	prevote1_1 := message.NewVote(message.PrevoteType, 1, 0, &proposedData)
	vs.Sign(prevote1_1)
	prevote1_2 := message.NewVote(message.PrevoteType, 1, 0, &proposedData)
	prevote1_2.Address = pubkey2
	prevote1_2.Signature = []byte(pubkey2)
	prevote1_3 := message.NewVote(message.PrevoteType, 1, 0, &proposedData)
	prevote1_3.Address = pubkey3
	prevote1_3.Signature = []byte(pubkey3)
	prevote1_4 := message.NewVote(message.PrevoteType, 1, 0, &proposedData)
	prevote1_4.Address = pubkey4
	prevote1_4.Signature = []byte(pubkey4)

	// test maj23
	hvSet1.AddVote(prevote1_1)
	data, ok := hvSet1.Prevotes(0).MinorQuorum()
	assert.False(ok)

	hvSet1.AddVote(prevote1_2)
	data, ok = hvSet1.Prevotes(0).MinorQuorum()
	assert.True(ok)
	data, ok = hvSet1.Prevotes(0).TwoThirdsMajority()
	assert.False(ok)
	assert.Equal(data, message.NilData)

	hvSet1.AddVote(prevote1_3)
	data, ok = hvSet1.Prevotes(0).TwoThirdsMajority()
	assert.True(ok)
	assert.Equal(data, proposedData)
}
