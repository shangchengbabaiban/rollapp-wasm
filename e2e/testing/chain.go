// DONTCOVER
package e2eTesting

import (
	"encoding/json"
	"math/rand"
	"strconv"
	"testing"
	"time"

	"github.com/CosmWasm/wasmd/x/wasm"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/codec"
	codecTypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptoCodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptoTypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/simulation"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsign "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authTypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	bankTypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashingTypes "github.com/cosmos/cosmos-sdk/x/slashing/types"
	stakingTypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/cosmos/ibc-go/v6/testing/mock"
	rollappparamstypes "github.com/dymensionxyz/dymension-rdk/x/rollappparams/types"
	seqtypes "github.com/dymensionxyz/dymension-rdk/x/sequencers/types"
	"github.com/dymensionxyz/dymint/version"
	"github.com/golang/protobuf/proto" //nolint:staticcheck
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmProto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmTypes "github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/dymensionxyz/rollapp-wasm/app"
)

var (
	TestAccountAddr = sdk.AccAddress("test")
	ProposerPK      = simapp.CreateTestPubKeys(1)[0]
	OperatorPK      = secp256k1.GenPrivKey().PubKey()
)

// TestChain keeps a test chain state and provides helper functions to simulate various operations.
// Heavily inspired by the TestChain from the ibc-go repo (https://github.com/cosmos/ibc-go/blob/main/testing/chain.go).
// Reasons for creating a custom TestChain rather than using the ibc-go's one are: to simplify it,
// add contract related helpers and fix errors caused by x/gastracker module (ibc-go version starts at block 2).
type TestChain struct {
	t *testing.T

	cfg         chainConfig
	app         *app.App                // main application
	lastHeader  tmProto.Header          // header for the last committed block
	curHeader   tmProto.Header          // header for the current block
	txConfig    client.TxConfig         // config to sing TXs
	valSet      *tmTypes.ValidatorSet   // validator set for the current block
	valSigners  []tmTypes.PrivValidator // validator signers for the current block
	accPrivKeys []cryptoTypes.PrivKey   // genesis account private keys
}

// NewTestChain creates a new TestChain with the default amount of genesis accounts and validators.
func NewTestChain(t *testing.T, chainIdx int, opts ...interface{}) *TestChain {
	chainid := "test-" + strconv.Itoa(chainIdx)

	pk, err := cryptocodec.ToTmProtoPublicKey(ProposerPK)
	require.NoError(t, err)

	// Split options by groups (each group is applied in a different init step)
	var chainCfgOpts []TestChainConfigOption
	// var consensusParamsOpts []TestChainConsensusParamsOption
	var genStateOpts []TestChainGenesisOption
	for i, opt := range opts {
		switch opt := opt.(type) {
		case TestChainConfigOption:
			chainCfgOpts = append(chainCfgOpts, opt)
			//	case TestChainConsensusParamsOption:
			//		consensusParamsOpts = append(consensusParamsOpts, opt)
		case TestChainGenesisOption:
			genStateOpts = append(genStateOpts, opt)
		default:
			require.Fail(t, "Unknown chain option type", "optionIdx", i)
		}
	}

	// Define chain config
	chainCfg := defaultChainConfig()
	for _, opt := range chainCfgOpts {
		opt(&chainCfg)
	}

	// Create an app and a default genesis state
	encCfg := app.MakeEncodingConfig()

	// Pick your poison here =)
	logger := log.NewNopLogger()
	if chainCfg.LoggerEnabled {
		logger = log.TestingLogger()
	}

	var emptyWasmOpts []wasm.Option
	rollApp := app.NewRollapp(
		logger,
		dbm.NewMemDB(),
		nil,
		true, map[int64]bool{},
		app.DefaultNodeHome,
		1,
		encCfg,
		app.GetEnabledProposals(),
		app.EmptyAppOptions{},
		emptyWasmOpts,
	)
	genState := app.NewDefaultGenesisState(rollApp.AppCodec())

	// Generate validators
	validators := make([]*tmTypes.Validator, 0, chainCfg.ValidatorsNum)
	valSigners := make([]tmTypes.PrivValidator, 0, chainCfg.ValidatorsNum)
	for i := 0; i < chainCfg.ValidatorsNum; i++ {
		valPrivKey := mock.NewPV()
		valPubKey, err := valPrivKey.GetPubKey()
		require.NoError(t, err)

		validators = append(validators, tmTypes.NewValidator(valPubKey, 1))
		valSigners = append(valSigners, valPrivKey)
	}
	validatorSet := tmTypes.NewValidatorSet(validators)

	// Generate genesis accounts, gen and bond coins
	genAccs := make([]authTypes.GenesisAccount, 0, chainCfg.GenAccountsNum)
	genAccPrivKeys := make([]cryptoTypes.PrivKey, 0, chainCfg.GenAccountsNum)
	for i := 0; i < chainCfg.GenAccountsNum; i++ {
		accPrivKey := secp256k1.GenPrivKey()
		acc := authTypes.NewBaseAccount(accPrivKey.PubKey().Address().Bytes(), accPrivKey.PubKey(), uint64(i), 0)

		genAccs = append(genAccs, acc)
		genAccPrivKeys = append(genAccPrivKeys, accPrivKey)
	}
	if chainCfg.DummyTestAddr {
		genAccs = append(genAccs, authTypes.NewBaseAccount(TestAccountAddr, nil, uint64(len(genAccs))-1, 0)) // deterministic account for testing purposes
	}

	genAmt, ok := sdk.NewIntFromString(chainCfg.GenBalanceAmount)
	require.True(t, ok)
	genCoins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, genAmt))

	bondAmt, ok := sdk.NewIntFromString(chainCfg.BondAmount)
	require.True(t, ok)
	bondCoins := sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, bondAmt))

	seqGenesis := seqtypes.GenesisState{
		Params: seqtypes.DefaultParams(),
	}
	genState[seqtypes.ModuleName] = rollApp.AppCodec().MustMarshalJSON(&seqGenesis)

	// Update the x/auth genesis with gen accounts
	authGenesis := authTypes.NewGenesisState(authTypes.DefaultParams(), genAccs)
	genState[authTypes.ModuleName] = rollApp.AppCodec().MustMarshalJSON(authGenesis)

	// Update the x/staking genesis (every gen account is a corresponding validator's delegator)
	stakingValidators := make([]stakingTypes.Validator, 0, len(validatorSet.Validators))
	stakingDelegations := make([]stakingTypes.Delegation, 0, len(validatorSet.Validators))
	for i, val := range validatorSet.Validators {
		valPubKey, err := cryptoCodec.FromTmPubKeyInterface(val.PubKey)
		require.NoError(t, err)

		valPubKeyAny, err := codecTypes.NewAnyWithValue(valPubKey)
		require.NoError(t, err)

		validator := stakingTypes.Validator{
			OperatorAddress:   sdk.ValAddress(val.Address).String(),
			ConsensusPubkey:   valPubKeyAny,
			Jailed:            false,
			Status:            stakingTypes.Bonded,
			Tokens:            bondAmt,
			DelegatorShares:   sdk.OneDec(),
			Description:       stakingTypes.Description{},
			UnbondingHeight:   int64(0),
			UnbondingTime:     time.Unix(0, 0).UTC(),
			Commission:        stakingTypes.NewCommission(sdk.ZeroDec(), sdk.ZeroDec(), sdk.ZeroDec()),
			MinSelfDelegation: sdk.ZeroInt(),
		}

		stakingValidators = append(stakingValidators, validator)
		stakingDelegations = append(stakingDelegations, stakingTypes.NewDelegation(genAccs[i].GetAddress(), val.Address.Bytes(), sdk.OneDec()))
	}

	stakingGenesis := stakingTypes.NewGenesisState(stakingTypes.DefaultParams(), stakingValidators, stakingDelegations)
	genState[stakingTypes.ModuleName] = rollApp.AppCodec().MustMarshalJSON(stakingGenesis)

	// Update x/bank genesis with total supply, gen account balances and bonding pool balance
	totalSupply := sdk.NewCoins()
	bondedPoolCoins := sdk.NewCoins()
	balances := make([]bankTypes.Balance, 0, chainCfg.GenAccountsNum)
	for i := 0; i < chainCfg.GenAccountsNum; i++ {
		accGenCoins := genCoins
		// Lower genesis balance for validator account
		if i < chainCfg.ValidatorsNum {
			accGenCoins = accGenCoins.Sub(bondCoins...)
			bondedPoolCoins = bondedPoolCoins.Add(bondCoins...)
		}

		balances = append(balances, bankTypes.Balance{
			Address: genAccs[i].GetAddress().String(),
			Coins:   accGenCoins,
		})
		totalSupply = totalSupply.Add(genCoins...)
	}

	if chainCfg.DummyTestAddr {
		balances = append(balances, bankTypes.Balance{
			Address: TestAccountAddr.String(), // add some balances to our dummy
			Coins:   genCoins,
		})
		totalSupply = totalSupply.Add(genCoins...)
	}

	balances = append(balances, bankTypes.Balance{
		Address: authTypes.NewModuleAddress(stakingTypes.BondedPoolName).String(),
		Coins:   bondedPoolCoins,
	})

	bankGenesis := bankTypes.NewGenesisState(bankTypes.DefaultGenesisState().Params, balances, totalSupply, []bankTypes.Metadata{
		{
			Description: "Stake token",
			DenomUnits: []*bankTypes.DenomUnit{
				{
					Denom:    "stake",
					Exponent: 18,
				},
			},
			Base:    "stake",
			Display: "stake",
		},
	})
	genState[bankTypes.ModuleName] = rollApp.AppCodec().MustMarshalJSON(bankGenesis)

	signInfo := make([]slashingTypes.SigningInfo, len(validatorSet.Validators))
	for i, v := range validatorSet.Validators {
		signInfo[i] = slashingTypes.SigningInfo{
			Address: sdk.ConsAddress(v.Address).String(),
			ValidatorSigningInfo: slashingTypes.ValidatorSigningInfo{
				Address: sdk.ConsAddress(v.Address).String(),
			},
		}
	}
	genState[slashingTypes.ModuleName] = rollApp.AppCodec().MustMarshalJSON(slashingTypes.NewGenesisState(slashingTypes.DefaultParams(), signInfo, nil))
	version.DRS = "1"

	rollappParamsState := rollappparamstypes.DefaultGenesisState()
	rollappParamsState.Params.DrsVersion = 1
	genState[rollappparamstypes.ModuleName] = rollApp.AppCodec().MustMarshalJSON(rollappParamsState)

	// Apply genesis options
	for _, opt := range genStateOpts {
		opt(rollApp.AppCodec(), genState)
	}

	// Apply consensus params options
	consensusParams := app.DefaultConsensusParams
	// for _, opt := range consensusParamsOpts {
	// 	opt(consensusParams)
	// }

	// Init chain
	genStateBytes, err := json.MarshalIndent(genState, "", " ")
	require.NoError(t, err)

	rollApp.InitChain(
		abci.RequestInitChain{
			Time:            time.Time{},
			ChainId:         chainid,
			ConsensusParams: consensusParams,
			Validators: []abci.ValidatorUpdate{
				{PubKey: pk, Power: 1},
			},
			AppStateBytes:   genStateBytes,
			InitialHeight:   0,
			GenesisChecksum: "notempty",
		},
	)

	// Create a chain and finalize the 1st block
	chain := TestChain{
		t:   t,
		cfg: chainCfg,
		app: rollApp,
		curHeader: tmProto.Header{
			ChainID: chainid,
			Time:    time.Unix(0, 0).UTC(),
		},
		txConfig:    encCfg.TxConfig,
		valSet:      validatorSet,
		valSigners:  valSigners,
		accPrivKeys: genAccPrivKeys,
	}
	chain.BeginBlock()
	chain.EndBlock()

	// Start a new block
	chain.BeginBlock()
	return &chain
}

// GetAccount returns account address and private key with the given index.
func (chain *TestChain) GetAccount(idx int) Account {
	t := chain.t

	require.Less(t, idx, len(chain.accPrivKeys))
	privKey := chain.accPrivKeys[idx]

	return Account{
		Address: sdk.AccAddress(privKey.PubKey().Address().Bytes()),
		PrivKey: privKey,
	}
}

// GetBalance returns the balance of the given account.
func (chain *TestChain) GetBalance(accAddr sdk.AccAddress) sdk.Coins {
	return chain.app.BankKeeper.GetAllBalances(chain.GetContext(), accAddr)
}

// GetModuleBalance returns the balance of the given module.
func (chain *TestChain) GetModuleBalance(moduleName string) sdk.Coins {
	ctx := chain.GetContext()
	moduleAcc := chain.app.AccountKeeper.GetModuleAccount(ctx, moduleName)

	return chain.app.BankKeeper.GetAllBalances(chain.GetContext(), moduleAcc.GetAddress())
}

// GetContext returns a context for the current block.
func (chain *TestChain) GetContext() sdk.Context {
	ctx := chain.app.BaseApp.NewContext(false, chain.curHeader)

	blockGasMeter := sdk.NewInfiniteGasMeter()
	blockMaxGas := chain.app.GetConsensusParams(ctx).Block.MaxGas
	if blockMaxGas >= 0 {
		blockGasMeter = sdk.NewGasMeter(sdk.Gas(blockMaxGas))
	}

	return ctx.WithBlockGasMeter(blockGasMeter)
}

// GetAppCodec returns the application codec.
func (chain *TestChain) GetAppCodec() codec.Codec {
	return chain.app.AppCodec()
}

// GetChainID returns the chain ID.
func (chain *TestChain) GetChainID() string {
	return chain.curHeader.ChainID
}

// GetBlockTime returns the current block time.
func (chain *TestChain) GetBlockTime() time.Time {
	return chain.curHeader.Time
}

// GetBlockHeight returns the current block height.
func (chain *TestChain) GetBlockHeight() int64 {
	return chain.app.LastBlockHeight()
}

// GetUnbondingTime returns x/staking validator unbonding time.
func (chain *TestChain) GetUnbondingTime() time.Duration {
	return chain.app.StakingKeeper.UnbondingTime(chain.GetContext())
}

// GetApp returns the application.
func (chain *TestChain) GetApp() *app.App {
	return chain.app
}

// NextBlock starts a new block with options time shift.
func (chain *TestChain) NextBlock(skipTime time.Duration) []abci.Event {
	ebEvents := chain.EndBlock()

	chain.curHeader.Time = chain.curHeader.Time.Add(skipTime)
	bbEvents := chain.BeginBlock()

	return append(ebEvents, bbEvents...)
}

func (chain *TestChain) GoToHeight(height int64, skipTime time.Duration) {
	if chain.GetBlockHeight() > height {
		panic("can't go to past height")
	}
	for chain.GetBlockHeight() < height {
		chain.NextBlock(skipTime)
	}
}

// BeginBlock begins a new block.
func (chain *TestChain) BeginBlock() []abci.Event {
	const blockDur = 5 * time.Second

	chain.lastHeader = chain.curHeader

	chain.curHeader.Height++
	chain.curHeader.Time = chain.curHeader.Time.Add(blockDur)
	chain.curHeader.AppHash = chain.app.LastCommitID().Hash
	chain.curHeader.ValidatorsHash = chain.valSet.Hash()
	chain.curHeader.NextValidatorsHash = chain.valSet.Hash()
	chain.curHeader.ProposerAddress = chain.GetCurrentValSet().Proposer.Address

	voteInfo := make([]abci.VoteInfo, len(chain.GetCurrentValSet().Validators))
	for i, v := range chain.GetCurrentValSet().Validators {
		voteInfo[i] = abci.VoteInfo{
			Validator: abci.Validator{
				Address: v.Address,
				Power:   v.VotingPower,
			},
			SignedLastBlock: true,
		}
	}

	res := chain.app.BeginBlock(abci.RequestBeginBlock{
		Hash:   nil,
		Header: chain.curHeader,
		LastCommitInfo: abci.LastCommitInfo{
			Round: 0,
			Votes: voteInfo,
		},
		ByzantineValidators: nil,
	})

	return res.Events
}

// EndBlock finalizes the current block.
func (chain *TestChain) EndBlock() []abci.Event {
	res := chain.app.EndBlock(abci.RequestEndBlock{Height: chain.curHeader.Height})
	chain.app.Commit()

	return res.Events
}

type (
	SendMsgOption func(opt *sendMsgOptions)

	sendMsgOptions struct {
		fees          sdk.Coins
		gasLimit      uint64
		noBlockChange bool
		simulate      bool
		granter       sdk.AccAddress
	}
)

func WithGranter(granter sdk.AccAddress) SendMsgOption {
	return func(opt *sendMsgOptions) {
		opt.granter = granter
	}
}

// WithMsgFees option add fees to the transaction.
func WithMsgFees(coins ...sdk.Coin) SendMsgOption {
	return func(opt *sendMsgOptions) {
		opt.fees = coins
	}
}

// WithTxGasLimit option overrides the default gas limit for the transaction.
func WithTxGasLimit(limit uint64) SendMsgOption {
	return func(opt *sendMsgOptions) {
		opt.gasLimit = limit
	}
}

// WithoutBlockChange option disables EndBlocker and BeginBlocker after the transaction.
func WithoutBlockChange() SendMsgOption {
	return func(opt *sendMsgOptions) {
		opt.noBlockChange = true
	}
}

// WithSimulation options estimates gas usage for the transaction.
func WithSimulation() SendMsgOption {
	return func(opt *sendMsgOptions) {
		opt.simulate = true
		opt.noBlockChange = true
	}
}

// SendMsgs sends a series of messages, checks for tx failure and starts a new block.
func (chain *TestChain) SendMsgs(senderAcc Account, expPass bool, msgs []sdk.Msg, opts ...SendMsgOption) (sdk.GasInfo, *sdk.Result, []abci.Event, error) {
	var abciEvents []abci.Event

	t := chain.t

	gasInfo, res, err := chain.SendMsgsRaw(senderAcc, msgs, opts...)
	if expPass {
		require.NoError(t, err)
		require.NotNil(t, res)
	} else {
		require.Error(t, err)
		require.Nil(t, res)
	}
	if res != nil {
		abciEvents = append(abciEvents, res.Events...)
	}

	if !chain.buildSendMsgOptions(opts...).noBlockChange {
		abciEvents = append(abciEvents, chain.EndBlock()...)
		abciEvents = append(abciEvents, chain.BeginBlock()...)
	}

	return gasInfo, res, abciEvents, err
}

// SendMsgsRaw sends a series of messages.
func (chain *TestChain) SendMsgsRaw(senderAcc Account, msgs []sdk.Msg, opts ...SendMsgOption) (sdk.GasInfo, *sdk.Result, error) {
	t := chain.t
	options := chain.buildSendMsgOptions(opts...)

	// Get the sender account
	senderAccI := chain.app.AccountKeeper.GetAccount(chain.GetContext(), senderAcc.Address)
	require.NotNil(t, senderAccI)

	// Build and sign Tx
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	tx, err := genSignedMockTx(
		r,
		chain.txConfig,
		msgs,
		options.fees,
		options.gasLimit,
		chain.GetChainID(),
		[]uint64{senderAccI.GetAccountNumber()},
		[]uint64{senderAccI.GetSequence()},
		[]cryptoTypes.PrivKey{senderAcc.PrivKey},
		options,
	)
	require.NoError(t, err)

	// Check the Tx
	if options.simulate {
		txBz, err := chain.txConfig.TxEncoder()(tx)
		require.NoError(t, err)

		return chain.app.Simulate(txBz)
	}

	// Send the Tx
	return chain.app.SimDeliver(chain.txConfig.TxEncoder(), tx)
}

// ParseSDKResultData converts TX result data into a slice of Msgs.
func (chain *TestChain) ParseSDKResultData(r *sdk.Result) sdk.TxMsgData {
	t := chain.t

	require.NotNil(t, r)

	var protoResult sdk.TxMsgData
	require.NoError(chain.t, proto.Unmarshal(r.Data, &protoResult))

	return protoResult
}

// GetDefaultTxFee returns the default transaction fee (that one is used if SendMsgs has no other options).
func (chain *TestChain) GetDefaultTxFee() sdk.Coins {
	t := chain.t

	feeAmt, ok := sdk.NewIntFromString(chain.cfg.DefaultFeeAmt)
	require.True(t, ok)

	return sdk.NewCoins(sdk.NewCoin(sdk.DefaultBondDenom, feeAmt))
}

func (chain *TestChain) buildSendMsgOptions(opts ...SendMsgOption) sendMsgOptions {
	options := sendMsgOptions{
		fees:          chain.GetDefaultTxFee(),
		gasLimit:      10_000_000,
		noBlockChange: false,
	}

	for _, o := range opts {
		o(&options)
	}

	return options
}

func genSignedMockTx(r *rand.Rand, txConfig client.TxConfig, msgs []sdk.Msg, feeAmt sdk.Coins, gas uint64, chainID string, accNums, accSeqs []uint64, priv []cryptoTypes.PrivKey, opt sendMsgOptions) (sdk.Tx, error) {
	sigs := make([]signing.SignatureV2, len(priv))

	// create a random length memo
	memo := simulation.RandStringOfLength(r, simulation.RandIntBetween(r, 0, 100))

	signMode := txConfig.SignModeHandler().DefaultMode()

	// 1st round: set SignatureV2 with empty signatures, to set correct
	// signer infos.
	for i, p := range priv {
		sigs[i] = signing.SignatureV2{
			PubKey: p.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode: signMode,
			},
			Sequence: accSeqs[i],
		}
	}

	tx := txConfig.NewTxBuilder()
	err := tx.SetMsgs(msgs...)
	if err != nil {
		return nil, err
	}
	err = tx.SetSignatures(sigs...)
	if err != nil {
		return nil, err
	}
	tx.SetMemo(memo)
	tx.SetFeeAmount(feeAmt)
	tx.SetGasLimit(gas)

	if opt.granter != nil {
		tx.SetFeeGranter(opt.granter)
	}

	// 2nd round: once all signer infos are set, every signer can sign.
	for i, p := range priv {
		signerData := authsign.SignerData{
			Address:       sdk.AccAddress(p.PubKey().Address()).String(),
			ChainID:       chainID,
			AccountNumber: accNums[i],
			Sequence:      accSeqs[i],
			PubKey:        p.PubKey(),
		}
		signBytes, err := txConfig.SignModeHandler().GetSignBytes(signMode, signerData, tx.GetTx())
		if err != nil {
			panic(err)
		}
		sig, err := p.Sign(signBytes)
		if err != nil {
			panic(err)
		}
		sigs[i].Data.(*signing.SingleSignatureData).Signature = sig
		err = tx.SetSignatures(sigs...)
		if err != nil {
			panic(err)
		}
	}

	return tx.GetTx(), nil
}
