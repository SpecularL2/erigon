package prover

import (
	"fmt"
	"math/big"

	gethcommon "github.com/ethereum/go-ethereum/common"
	gethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/holiman/uint256"
	"github.com/ledgerwatch/erigon-lib/chain"
	libcommon "github.com/ledgerwatch/erigon-lib/common"
	"github.com/ledgerwatch/erigon-lib/kv"
	"github.com/ledgerwatch/erigon-lib/kv/memdb"
	"github.com/ledgerwatch/erigon/core/state"
	"github.com/ledgerwatch/erigon/core/types/accounts"
	"github.com/ledgerwatch/erigon/turbo/trie"
	prover_types "github.com/specularl2/specular/clients/geth/specular/prover/types"
)

type ErigonGlobalStateWrapper struct {
	rules  *chain.Rules
	ibs    *state.IntraBlockState
	tx     kv.Tx
	reader state.StateReader
	writer state.StateWriter
}

func NewErigonGlobalStateWrapper(
	rules *chain.Rules,
	ibs *state.IntraBlockState,
	tx kv.Tx,
	reader state.StateReader,
	writer state.StateWriter,
) *ErigonGlobalStateWrapper {
	return &ErigonGlobalStateWrapper{
		rules:  rules,
		ibs:    ibs,
		tx:     tx,
		reader: reader,
		writer: writer,
	}
}

func (sw *ErigonGlobalStateWrapper) Copy() prover_types.L2ELClientStateInterface {
	tx := memdb.NewMemoryBatch(sw.tx, "temp")
	reader := state.NewPlainStateReader(tx)
	writer := state.NewPlainStateWriterNoHistory(tx)
	s := &ErigonGlobalStateWrapper{
		rules:  sw.rules,
		ibs:    sw.ibs.Copy(reader),
		tx:     tx,
		reader: reader,
		writer: writer,
	}
	// runtime.SetFinalizer(s, func(s *ErigonGlobalStateWrapper) {
	// 	// runtime.SetFinalizer(s, nil)
	// 	// thread safe?
	// 	s.tx.Rollback()
	// })
	return s
}

func (sw *ErigonGlobalStateWrapper) GetRootForProof() gethcommon.Hash {
	root, err := trie.CalcRoot("ErigonGlobalStateWrapper::GetRootForProof", sw.tx)
	if err != nil {
		panic(err)
	}
	return gethcommon.Hash(root)
}

func (sw *ErigonGlobalStateWrapper) CommitForProof() {
	sw.ibs.CommitForProof(sw.rules, sw.writer)
}

func (sw *ErigonGlobalStateWrapper) GetCurrentLogs() []*gethtypes.Log {
	logs := sw.ibs.GetCurrentLogs()
	gethLogs := make([]*gethtypes.Log, len(logs))
	for i, log := range logs {
		topics := make([]gethcommon.Hash, len(log.Topics))
		for j, topic := range log.Topics {
			topics[j] = gethcommon.Hash(topic)
		}
		gethLogs[i] = &gethtypes.Log{
			Address:     gethcommon.Address(log.Address),
			Topics:      topics,
			Data:        log.Data,
			BlockNumber: log.BlockNumber,
			TxHash:      gethcommon.Hash(log.TxHash),
			TxIndex:     log.TxIndex,
			BlockHash:   gethcommon.Hash(log.BlockHash),
			Index:       log.Index,
			Removed:     log.Removed,
		}
	}
	return gethLogs
}

// Pre-condition: ibs finalized, i.e. updates are written to the db
// so reader can read them
func (sw *ErigonGlobalStateWrapper) GetProof(gethAddr gethcommon.Address) ([][]byte, error) {
	addr := libcommon.Address(gethAddr)
	rl := trie.NewRetainList(0)
	a, err := sw.reader.ReadAccountData(addr)
	if err != nil {
		return nil, err
	}
	if a == nil {
		a = &accounts.Account{}
	}
	loader := trie.NewFlatDBTrieLoader("ErigonGlobalStateWrapper::GetProof", rl, nil, nil, false)
	pr, err := trie.NewProofRetainer(addr, a, nil, rl)
	if err != nil {
		return nil, err
	}
	loader.SetProofRetainer(pr)
	_, err = loader.CalcTrieRoot(sw.tx, nil)
	if err != nil {
		return nil, err
	}
	res, err := pr.ProofResult()
	if err != nil {
		return nil, err
	}
	proof := make([][]byte, len(res.AccountProof))
	for i, p := range res.AccountProof {
		proof[i] = p
	}
	fmt.Println("account proof", proof)
	return proof, nil
}

func (sw *ErigonGlobalStateWrapper) GetStorageProof(gethAddr gethcommon.Address, gethKey gethcommon.Hash) ([][]byte, error) {
	addr := libcommon.Address(gethAddr)
	key := libcommon.Hash(gethKey)

	rl := trie.NewRetainList(0)
	a, err := sw.reader.ReadAccountData(addr)
	if err != nil {
		return nil, err
	}
	if a == nil {
		a = &accounts.Account{}
	}
	loader := trie.NewFlatDBTrieLoader("ErigonGlobalStateWrapper::GetStorageProof", rl, nil, nil, false)
	pr, err := trie.NewProofRetainer(addr, a, []libcommon.Hash{key}, rl)
	if err != nil {
		return nil, err
	}
	loader.SetProofRetainer(pr)
	_, err = loader.CalcTrieRoot(sw.tx, nil)
	if err != nil {
		return nil, err
	}
	res, err := pr.ProofResult()
	if err != nil {
		return nil, err
	}
	if len(res.StorageProof) != 1 {
		return nil, fmt.Errorf("expected 1 storage proof, got %d", len(res.StorageProof))
	}
	proof := make([][]byte, len(res.StorageProof[0].Proof))
	for i, p := range res.StorageProof[0].Proof {
		proof[i] = p
	}
	fmt.Println("storage proof", proof)
	return proof, nil
}

func (sw *ErigonGlobalStateWrapper) DeleteSuicidedAccountForProof(addr gethcommon.Address) {
	sw.ibs.DeleteSuicidedAccountForProof(libcommon.Address(addr), sw.writer)
}

// Direct implementations

func (sw *ErigonGlobalStateWrapper) Prepare(thash, bhash gethcommon.Hash, ti int) {
	sw.ibs.SetTxContext(libcommon.Hash(thash), libcommon.Hash(bhash), ti)
}

func (sw *ErigonGlobalStateWrapper) GetRefund() uint64 {
	return sw.ibs.GetRefund()
}

func (sw *ErigonGlobalStateWrapper) GetCode(address gethcommon.Address) []byte {
	return sw.ibs.GetCode(libcommon.Address(address))
}

func (sw *ErigonGlobalStateWrapper) SubBalance(address gethcommon.Address, amount *big.Int) {
	amount_, success := uint256.FromBig(amount)
	if !success {
		panic("failed to convert big.Int to uint256.Int")
	}
	sw.ibs.SubBalance(libcommon.Address(address), amount_)
}

func (sw *ErigonGlobalStateWrapper) SetNonce(address gethcommon.Address, nonce uint64) {
	sw.ibs.SetNonce(libcommon.Address(address), nonce)
}

func (sw *ErigonGlobalStateWrapper) GetNonce(address gethcommon.Address) uint64 {
	return sw.ibs.GetNonce(libcommon.Address(address))
}

func (sw *ErigonGlobalStateWrapper) AddBalance(address gethcommon.Address, amount *big.Int) {
	amount_, success := uint256.FromBig(amount)
	if !success {
		panic("failed to convert big.Int to uint256.Int")
	}
	sw.ibs.AddBalance(libcommon.Address(address), amount_)
}

func (sw *ErigonGlobalStateWrapper) SetCode(address gethcommon.Address, code []byte) {
	sw.ibs.SetCode(libcommon.Address(address), code)
}

func (sw *ErigonGlobalStateWrapper) GetBalance(address gethcommon.Address) *big.Int {
	return sw.ibs.GetBalance(libcommon.Address(address)).ToBig()
}

func (sw *ErigonGlobalStateWrapper) GetCodeHash(address gethcommon.Address) gethcommon.Hash {
	codeHash := sw.ibs.GetCodeHash(libcommon.Address(address))
	return gethcommon.Hash(codeHash)
}
