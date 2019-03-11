# gobft
gobft is a BFT library written in go. It's a re-implementation of tendermint bft.
![cmd-markdown-logo](resource/tmbft.jpeg)

# Terminology
* validator: all the nodes in a distributed system that participate in the consensus process
* POL: proof of lock. A validator reaches POL for a certain proposal when it receives
  +2/3 prevote for this proposal and precommit it. Once it reaches POL, the validator 
  can only prevote/precommit the POLed proposal in later rounds until it has a PoLC
* PoLC: proof of lock change (unlock previous POLed proposal). A validator reaches PoLC
  when it has +2/3 prevote for a different proposal in a higher round.
* To prevote or precommit something means to broadcast a prevote vote or precommit
  vote for something. e.g. prevotePOL means to broadcast a prevote vote for a POL
  proposal. prevoteNIL means to broadcast a prevote vote for nothing.
* +2/3 means a proposal got the vote from more than 2/3 of all the validators 
  in a certain round
* 2/3 Any: more than 2/3 of all the validators broadcast their votes, yet no proposal
  reached +2/3 (i.e. the validators voted for different proposals)
  
  A validator is a node in a distributed system that participates in the
  bft consensus process. It proposes and votes for a certain proposal.
  Each validator should maintain a set of all the PubValidators so that
  it can verifies messages sent by other validators. Each validator should
  have exactly one PrivValidator which contains its private key so that
  it can sign a message. A validator can be a proposer, the rules of which
  validator becomes a valid proposer at a certain time is totally decided by user.
  
  To get more details of POL, please refer to [tendermint.pdf](https://allquantor.at/blockchainbib/pdf/buchman2016tendermint.pdf) chapter 3

## Note
`IPubValidator` and `IPrivValidator` interface provides a abstract access of user-defined
  signature type(e.g. ecc or rsa)
  
 In a nutshell, user should implement the following interface:
 ```go
// ICommittee represents a validator group which contains all validators at
// a certain height. User should typically create a new ICommittee and register
// it to the bft core before starting a new height consensus process if
// validators need to be updated
type ICommittee interface {
	GetValidator(key message.PubKey) IPubValidator
	IsValidator(key message.PubKey) bool
	TotalVotingPower() int64

	GetCurrentProposer(round int) message.PubKey
	// DecidesProposal decides what will be proposed if this validator is the current
	// proposer.
	DecidesProposal() message.ProposedData

	// ValidateProposed validates the proposed data
	ValidateProposed(data message.ProposedData) bool

	// Commit defines the actions the users taken when consensus is reached
	Commit(p message.ProposedData) error

	GetAppState() *message.AppState
	// BroadCast sends msg to other validators
	BroadCast(msg message.ConsensusMessage) error
}

// IPubValidator verifies if a message is properly signed by the right validator
type IPubValidator interface {
	VerifySig(digest, signature []byte) bool
	GetPubKey() message.PubKey
	GetVotingPower() int64
	SetVotingPower(int64)
}

// IPrivValidator signs a message
type IPrivValidator interface {
	GetPubKey() message.PubKey
	Sign(digest []byte) []byte
}
```

# Data flow and state transition
![cmd-markdown-logo](resource/goBFT-dataflow.jpeg)

# TODO list
- [ ] **system halt and recover**
The whole network might halt and stop voting process under extreme circumstances. Taking the following scecario for example:
There are 4 validators(A, B, C, D) in totally, in the first round of the vote, both A and B received +2/3 prevotes for proposal `x`. So they broadcasted precommits and locked on `x`. Meanwhile C and D didn't get +2/3 prevotes due to network issues so they broadcasted precommits for `nil`. No consensus can be reached in this round so all 4 validators started a new round. In this round, another validator was chosen as the current proposer and it proposed `y`. Since A is locked on `x`, A prevoted for `x`. C and D didn't lock on any previous proposal so they prevoted for `y`. Now say B is byzantine and it prevoted for `y`. Now C and D saw +2/3 prevotes for `y` and they both precommitted for `y` and locked on it. If any of prevotes for `y` didn't reach A due to network issue and B deliberately shut himself down, the rest 3 validators was locked on 2 different proposals and will never reach consensus.
> The above example has 1 malicious node and 1 node with network failure so there're 2 byzantine validators out of 4, which is more than what bft protocol can handle. With 1 byzantine validator, bft can work well only if there's no message loss during network transmission. However it's too naive to make such assumption in real life. Additional mechanism should be introduce so that validators can detect the halt and recover from it.

- [ ] **message retransmission**
gobft requires that at least +2/3 of the vote messages are successfully delivered to at least +2/3 validators. Validators should be able to proactively request missing votes from other validators.

- [ ] **special handling when number of validators is < 3**
The mininum requirement of validators now is 3. 