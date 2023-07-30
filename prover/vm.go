package prover

import (
	"github.com/ledgerwatch/erigon-lib/chain"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/vm"
	prover_types "github.com/specularl2/specular/clients/geth/specular/prover/types"
)

type ErigonVMWrapper struct {
	vm     vm.VMInterface
	rules  *chain.Rules
	tx     kv.Tx
	reader state.StateReader
	writer state.StateWriter
}

func NewErigonVMWrapper(
	vm vm.VMInterface,
	rules *chain.Rules,
	tx kv.Tx,
	reader state.StateReader,
	writer state.StateWriter,
) *ErigonVMWrapper {
	return &ErigonVMWrapper{vm, rules, tx, reader, writer}
}

func (vmw *ErigonVMWrapper) Context() prover_types.L2ELClientBlockContextInterface {
	bctx := vmw.vm.Context()
	return &ErigonBlockContextWrapper{&bctx}
}

func (vmw *ErigonVMWrapper) StateDB() prover_types.L2ELClientStateInterface {
	ibs := vmw.vm.IntraBlockState()
	return NewErigonGlobalStateWrapper(vmw.rules, ibs.(*state.IntraBlockState), vmw.tx, vmw.reader, vmw.writer)
}
