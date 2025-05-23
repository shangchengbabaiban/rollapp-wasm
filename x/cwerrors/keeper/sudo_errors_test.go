package keeper_test

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	e2eTesting "github.com/dymensionxyz/rollapp-wasm/e2e/testing"
	"github.com/dymensionxyz/rollapp-wasm/pkg/testutils"
	"github.com/dymensionxyz/rollapp-wasm/x/cwerrors/types"
)

func (s *KeeperTestSuite) TestSetError() {
	// Setting up chain and contract in mock wasm keeper
	ctx, keeper := s.chain.GetContext(), s.chain.GetApp().CWErrorsKeeper
	contractViewer := testutils.NewMockContractViewer()
	keeper.SetWasmKeeper(contractViewer)
	contractAddresses := e2eTesting.GenContractAddresses(3)
	contractAddr := contractAddresses[0]
	contractAddr2 := contractAddresses[1]
	contractAdminAcc := s.chain.GetAccount(0)
	contractViewer.AddContractAdmin(
		contractAddr.String(),
		contractAdminAcc.Address.String(),
	)
	testCases := []struct {
		testCase    string
		sudoError   types.SudoError
		expectError bool
		errorType   error
	}{
		{
			testCase: "FAIL: contract address is invalid",
			sudoError: types.SudoError{
				ContractAddress: "👻",
				ModuleName:      "test",
				ErrorCode:       1,
				InputPayload:    "test",
				ErrorMessage:    "test",
			},
			expectError: true,
			errorType:   fmt.Errorf("decoding bech32 failed: invalid bech32 string length 4"),
		},
		{
			testCase: "FAIL: module name is invalid",
			sudoError: types.SudoError{
				ContractAddress: contractAddr.String(),
				ModuleName:      "",
				ErrorCode:       1,
				InputPayload:    "test",
				ErrorMessage:    "test",
			},
			expectError: true,
			errorType:   types.ErrModuleNameMissing,
		},
		{
			testCase: "FAIL: contract does not exist",
			sudoError: types.SudoError{
				ContractAddress: contractAddr2.String(),
				ModuleName:      "test",
				ErrorCode:       1,
				InputPayload:    "test",
				ErrorMessage:    "test",
			},
			expectError: true,
			errorType:   types.ErrContractNotFound,
		},
		{
			testCase: "OK: successfully set error",
			sudoError: types.SudoError{
				ContractAddress: contractAddr.String(),
				ModuleName:      "test",
				ErrorCode:       1,
				InputPayload:    "test",
				ErrorMessage:    "test",
			},
			expectError: false,
		},
	}
	for _, tc := range testCases {
		s.Run(fmt.Sprintf("Case: %s", tc.testCase), func() {
			err := keeper.SetError(ctx, tc.sudoError)
			if tc.expectError {
				s.Require().Error(err)
				s.Assert().ErrorContains(err, tc.errorType.Error())
			} else {
				s.Require().NoError(err)

				getErrors, err := keeper.GetErrorsByContractAddress(ctx, sdk.MustAccAddressFromBech32(tc.sudoError.ContractAddress))
				s.Require().NoError(err)
				s.Require().Len(getErrors, 1)
				s.Require().Equal(tc.sudoError, getErrors[0])
			}
		})
	}
}

func (s *KeeperTestSuite) TestGetErrorsByContractAddress() {
	// Setting up chain and contract in mock wasm keeper
	ctx, keeper := s.chain.GetContext(), s.chain.GetApp().CWErrorsKeeper
	contractViewer := testutils.NewMockContractViewer()
	keeper.SetWasmKeeper(contractViewer)
	contractAddresses := e2eTesting.GenContractAddresses(3)
	contractAddr := contractAddresses[0]
	contractAddr2 := contractAddresses[1]
	contractAdminAcc := s.chain.GetAccount(0)
	contractViewer.AddContractAdmin(
		contractAddr.String(),
		contractAdminAcc.Address.String(),
	)
	contractViewer.AddContractAdmin(
		contractAddr2.String(),
		contractAdminAcc.Address.String(),
	)
	// Set errors for block 1
	// 2 errors for contract1
	// 1 error for contract2
	contract1Err := types.SudoError{
		ContractAddress: contractAddr.String(),
		ModuleName:      "test",
	}
	contract2Err := types.SudoError{
		ContractAddress: contractAddr2.String(),
		ModuleName:      "test",
	}
	err := keeper.SetError(ctx, contract1Err)
	s.Require().NoError(err)
	err = keeper.SetError(ctx, contract1Err)
	s.Require().NoError(err)
	err = keeper.SetError(ctx, contract2Err)
	s.Require().NoError(err)

	// Check number of errors match
	sudoErrs, err := keeper.GetErrorsByContractAddress(ctx, contractAddr.Bytes())
	s.Require().NoError(err)
	s.Require().Len(sudoErrs, 2)
	sudoErrs, err = keeper.GetErrorsByContractAddress(ctx, contractAddr2.Bytes())
	s.Require().NoError(err)
	s.Require().Len(sudoErrs, 1)

	// Increment block height
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)

	// Set errors for block 2
	// 1 error for contract1
	// 1 error for contract2
	err = keeper.SetError(ctx, contract1Err)
	s.Require().NoError(err)
	err = keeper.SetError(ctx, contract2Err)
	s.Require().NoError(err)

	// Check number of errors match
	sudoErrs, err = keeper.GetErrorsByContractAddress(ctx, contractAddr.Bytes())
	s.Require().NoError(err)
	s.Require().Len(sudoErrs, 3)
	sudoErrs, err = keeper.GetErrorsByContractAddress(ctx, contractAddr2.Bytes())
	s.Require().NoError(err)
	s.Require().Len(sudoErrs, 2)
}

func (s *KeeperTestSuite) TestPruneErrorsByBlockHeight() {
	// Setting up chain and contract in mock wasm keeper
	ctx, keeper := s.chain.GetContext(), s.chain.GetApp().CWErrorsKeeper
	contractViewer := testutils.NewMockContractViewer()
	keeper.SetWasmKeeper(contractViewer)
	contractAddresses := e2eTesting.GenContractAddresses(3)
	contractAddr := contractAddresses[0]
	contractAddr2 := contractAddresses[1]
	contractAdminAcc := s.chain.GetAccount(0)
	contractViewer.AddContractAdmin(
		contractAddr.String(),
		contractAdminAcc.Address.String(),
	)
	contractViewer.AddContractAdmin(
		contractAddr2.String(),
		contractAdminAcc.Address.String(),
	)

	// Set errors for block 1
	contract1Err := types.SudoError{
		ContractAddress: contractAddr.String(),
		ModuleName:      "test",
	}
	contract2Err := types.SudoError{
		ContractAddress: contractAddr2.String(),
		ModuleName:      "test",
	}

	// Set errors for block 1
	// 1 errors for contract1
	// 1 error for contract2
	err := keeper.SetError(ctx, contract1Err)
	s.Require().NoError(err)
	err = keeper.SetError(ctx, contract2Err)
	s.Require().NoError(err)

	// Calculate height at which these errors are pruned
	params, err := keeper.GetParams(ctx)
	s.Require().NoError(err)
	pruneHeight := ctx.BlockHeight() + params.GetErrorStoredTime()

	// Increment block height
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	// Set errors for block 2
	// 1 error for contract1
	err = keeper.SetError(ctx, contract1Err)
	s.Require().NoError(err)

	// Check number of errors match
	getErrors, err := keeper.GetErrorsByContractAddress(ctx, contractAddr.Bytes())
	s.Require().NoError(err)
	s.Require().Len(getErrors, 2)
	getErrors, err = keeper.GetErrorsByContractAddress(ctx, contractAddr2.Bytes())
	s.Require().NoError(err)
	s.Require().Len(getErrors, 1)

	// Go to prune height and prune errors
	ctx = ctx.WithBlockHeight(pruneHeight)
	err = keeper.PruneErrorsCurrentBlock(ctx)
	s.Require().NoError(err)

	// Check number of errors match
	getErrors, err = keeper.GetErrorsByContractAddress(ctx, contractAddr.Bytes())
	s.Require().NoError(err)
	s.Require().Len(getErrors, 1)
	getErrors, err = keeper.GetErrorsByContractAddress(ctx, contractAddr2.Bytes())
	s.Require().NoError(err)
	s.Require().Len(getErrors, 0)

	// Increment block height + add error for contract 2 + prune
	ctx = ctx.WithBlockHeight(ctx.BlockHeight() + 1)
	err = keeper.SetError(ctx, contract2Err)
	s.Require().NoError(err)
	err = keeper.PruneErrorsCurrentBlock(ctx)
	s.Require().NoError(err)

	// Check number of errors match
	getErrors, err = keeper.GetErrorsByContractAddress(ctx, contractAddr.Bytes())
	s.Require().NoError(err)
	s.Require().Len(getErrors, 0)
	getErrors, err = keeper.GetErrorsByContractAddress(ctx, contractAddr2.Bytes())
	s.Require().NoError(err)
	s.Require().Len(getErrors, 1)
}

func (s *KeeperTestSuite) TestSudoErrorCallback() {
	// Setting up chain and contract in mock wasm keeper
	ctx, keeper := s.chain.GetContext(), s.chain.GetApp().CWErrorsKeeper

	sudoErr := types.SudoError{
		ContractAddress: e2eTesting.GenContractAddresses(1)[0].String(),
		ModuleName:      "test",
		ErrorCode:       1,
		InputPayload:    "test",
		ErrorMessage:    "test",
	}

	// Set error
	keeper.SetSudoErrorCallback(ctx, 1, sudoErr)
	keeper.SetSudoErrorCallback(ctx, 2, sudoErr)

	// Get error
	getErrs := keeper.GetAllSudoErrorCallbacks(ctx)
	s.Require().Len(getErrs, 2)

	// Go to next block and check if errors are 0 as this is a transient store
	s.chain.NextBlock(1)

	getErrs = keeper.GetAllSudoErrorCallbacks(s.chain.GetContext())
	s.Require().Len(getErrs, 0)
}
