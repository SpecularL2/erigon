package prover

import (
	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	gethvm "github.com/ethereum/go-ethereum/core/vm"
	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/chain"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/vm"
	"github.com/ledgerwatch/erigon/core/vm/evmtypes"
	specularprover "github.com/specularl2/specular/clients/geth/specular/prover"
	specularstate "github.com/specularl2/specular/clients/geth/specular/prover/state"
	prover_types "github.com/specularl2/specular/clients/geth/specular/prover/types"
)

// TODO: import prover here

type BenchProver struct {
	prover *specularprover.BenchProver
	rules  *chain.Rules
	dbtx   kv.Tx
	reader state.StateReader
	writer state.StateWriter
}

func NewBenchProver(
	transaction prover_types.Transaction,
	txctx *evmtypes.TxContext,
	receipt *gethtypes.Receipt,
	rules *chain.Rules,
	blockNumber uint64,
	transactionIdx uint64,
	dbtx kv.Tx,
	reader state.StateReader,
	writer state.StateWriter,
	committedGlobalState *ErigonGlobalStateWrapper,
	interState specularstate.InterState,
	blockHashTree *specularstate.BlockHashTree,
) (*BenchProver, error) {
	prover := specularprover.NewBenchProver(
		transaction,
		toGethTxContext(txctx),
		receipt,
		toGethRules(rules),
		blockNumber,
		transactionIdx,
		committedGlobalState,
		interState,
		blockHashTree,
	)
	return &BenchProver{prover, rules, dbtx, reader, writer}, nil
}

func (p *BenchProver) CaptureTxStart(gasLimit uint64) {
	p.prover.CaptureTxStart(gasLimit)
}

func (p *BenchProver) CaptureTxEnd(restGas uint64) {
	p.prover.CaptureTxEnd(restGas)
}

func (p *BenchProver) CaptureStart(env vm.VMInterface, from libcommon.Address, to libcommon.Address, precompile bool, create bool, input []byte, gas uint64, value *uint256.Int, code []byte) {
	envw := NewErigonVMWrapper(env, p.rules, p.dbtx, p.reader, p.writer)
	p.prover.CaptureStartImpl(envw, gethcommon.Address(from), gethcommon.Address(to), create, input, gas, value.ToBig())
}

func (p *BenchProver) CaptureEnd(output []byte, usedGas uint64, err error) {
	p.prover.CaptureEnd(output, usedGas, 0, err)
}

func (p *BenchProver) CaptureEnter(typ vm.OpCode, from libcommon.Address, to libcommon.Address, precompile bool, create bool, input []byte, gas uint64, value *uint256.Int, code []byte) {
	p.prover.CaptureEnter(gethvm.OpCode(typ), gethcommon.Address(from), gethcommon.Address(to), input, gas, value.ToBig())
}

func (p *BenchProver) CaptureExit(output []byte, usedGas uint64, err error) {
	p.prover.CaptureExit(output, usedGas, err)
}

func (p *BenchProver) CaptureState(pc uint64, op vm.OpCode, gas, cost uint64, scope *vm.ScopeContext, rData []byte, depth int, err error) {
	scopew := &ErigonScopeContextWrapper{scope}
	p.prover.CaptureStateImpl(pc, gethvm.OpCode(op), gas, cost, scopew, rData, depth, err)
}

func (p *BenchProver) CaptureFault(pc uint64, op vm.OpCode, gas, cost uint64, scope *vm.ScopeContext, depth int, err error) {
	scopew := &ErigonScopeContextWrapper{scope}
	p.prover.CaptureFaultImpl(pc, gethvm.OpCode(op), gas, cost, scopew, depth, err)
}

func (p *BenchProver) GetResult() ([]byte, error) {
	return p.prover.GetResult()
}
