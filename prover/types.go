package prover

import (
	"fmt"
	"math/big"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/chain"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon/core/types"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
	"github.com/ledgerwatch/erigon/core/vm/stack"
	prover_types "github.com/specularl2/specular/clients/geth/specular/prover/types"
)

type ErigonTransactionWrapper struct {
	types.Transaction
}

func (tx *ErigonTransactionWrapper) AccessList() gethtypes.AccessList {
	erigon_acl := tx.Transaction.GetAccessList()
	geth_acl := make(gethtypes.AccessList, len(erigon_acl))
	for i, erigon_acl_entry := range erigon_acl {
		geth_acl[i] = gethtypes.AccessTuple{
			Address:     gethcommon.Address(erigon_acl_entry.Address),
			StorageKeys: make([]gethcommon.Hash, len(erigon_acl_entry.StorageKeys)),
		}
		for j, erigon_acl_entry_storage_key := range erigon_acl_entry.StorageKeys {
			geth_acl[i].StorageKeys[j] = gethcommon.Hash(erigon_acl_entry_storage_key)
		}
	}
	return geth_acl
}

func (tx *ErigonTransactionWrapper) AsMessage(s interface{}, baseFee *big.Int, rules interface{}) (interface{}, error) {
	return tx.Transaction.AsMessage(s.(types.Signer), baseFee, rules.(*chain.Rules))
}

func (tx *ErigonTransactionWrapper) ChainId() *big.Int {
	return tx.Transaction.GetChainID().ToBig()
}

func (tx *ErigonTransactionWrapper) Data() []byte {
	return tx.Transaction.GetData()
}

func (tx *ErigonTransactionWrapper) Gas() uint64 {
	return tx.Transaction.GetGas()
}

func (tx *ErigonTransactionWrapper) GasPrice() *big.Int {
	return tx.Transaction.GetPrice().ToBig()
}

func (tx *ErigonTransactionWrapper) Hash() gethcommon.Hash {
	return gethcommon.Hash(tx.Transaction.Hash())
}

func (tx *ErigonTransactionWrapper) Nonce() uint64 {
	return tx.Transaction.GetNonce()
}

func (tx *ErigonTransactionWrapper) To() *gethcommon.Address {
	to := tx.Transaction.GetTo()
	if to == nil {
		return nil
	}
	to_addr := gethcommon.Address(*to)
	return &to_addr
}

func (tx *ErigonTransactionWrapper) Value() *big.Int {
	return tx.Transaction.GetValue().ToBig()
}

func (tx *ErigonTransactionWrapper) RawSignatureValues() (*big.Int, *big.Int, *big.Int) {
	v, r, s := tx.Transaction.RawSignatureValues()
	return v.ToBig(), r.ToBig(), s.ToBig()
}

func toGethTransaction(tx types.Transaction) prover_types.Transaction {
	return &ErigonTransactionWrapper{tx}
}

type ErigonBlockContextWrapper struct {
	*evmtypes.BlockContext
}

func (bctx *ErigonBlockContextWrapper) CanTransfer() prover_types.CanTransferFunc {
	return func(state prover_types.L2ELClientStateInterface, addr gethcommon.Address, gethamount *big.Int) bool {
		amount, ok := uint256.FromBig(gethamount)
		if !ok {
			panic(fmt.Sprintf("CanTransfer: amount %v is too big", gethamount))
		}
		return bctx.BlockContext.CanTransfer(state.(evmtypes.IntraBlockState), libcommon.Address(addr), amount)
	}
}

func (bctx *ErigonBlockContextWrapper) GetHash() prover_types.GetHashFunc {
	return func(n uint64) gethcommon.Hash {
		return gethcommon.Hash(bctx.BlockContext.GetHash(n))
	}
}

func (bctx *ErigonBlockContextWrapper) Coinbase() gethcommon.Address {
	return gethcommon.Address(bctx.BlockContext.Coinbase)
}

func (bctx *ErigonBlockContextWrapper) GasLimit() uint64 {
	return bctx.BlockContext.GasLimit
}

func (bctx *ErigonBlockContextWrapper) BlockNumber() *big.Int {
	return new(big.Int).SetUint64(bctx.BlockContext.BlockNumber)
}

func (bctx *ErigonBlockContextWrapper) Time() *big.Int {
	return new(big.Int).SetUint64(bctx.BlockContext.Time)
}

func (bctx *ErigonBlockContextWrapper) Difficulty() *big.Int {
	return bctx.BlockContext.Difficulty
}

func (bctx *ErigonBlockContextWrapper) BaseFee() *big.Int {
	return bctx.BlockContext.BaseFee.ToBig()
}

func (bctx *ErigonBlockContextWrapper) Random() *gethcommon.Hash {
	if bctx.BlockContext.PrevRanDao == nil {
		return nil
	}
	random := gethcommon.Hash(*bctx.BlockContext.PrevRanDao)
	return &random
}

type ErigonStackWrapper struct {
	*stack.Stack
}

func (stack *ErigonStackWrapper) Data() []uint256.Int {
	return stack.Stack.Data
}

type ErigonContractWrapper struct {
	*vm.Contract
}

func (contract *ErigonContractWrapper) Caller() gethcommon.Address {
	return gethcommon.Address(contract.Contract.Caller())
}

func (contract *ErigonContractWrapper) Address() gethcommon.Address {
	return gethcommon.Address(contract.Contract.Address())
}

func (contract *ErigonContractWrapper) Value() *big.Int {
	return contract.Contract.Value().ToBig()
}

func (contract *ErigonContractWrapper) Code() []byte {
	return contract.Contract.Code
}

type ErigonScopeContextWrapper struct {
	*vm.ScopeContext
}

func (sctx *ErigonScopeContextWrapper) Memory() prover_types.L2ELClientMemoryInterface {
	return sctx.ScopeContext.Memory
}

func (sctx *ErigonScopeContextWrapper) Stack() prover_types.L2ELClientStackInterface {
	return &ErigonStackWrapper{sctx.ScopeContext.Stack}
}

func (sctx *ErigonScopeContextWrapper) Contract() prover_types.L2ELClientContractInterface {
	return &ErigonContractWrapper{sctx.ScopeContext.Contract}
}
