package cstesting

import (
	"crypto/ecdsa"
	"fmt"
	"math/big"

	"github.com/ethereum/go-ethereum/accounts/abi/bind"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/core/types"
	"github.com/nodeset-org/osha/keys"
	"github.com/rocket-pool/node-manager-core/eth"
	"github.com/rocket-pool/rocketpool-go/v2/dao/oracle"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
	"github.com/rocket-pool/rocketpool-go/v2/tokens"
)

// =============
// === Nodes ===
// =============

// Register multiple nodes at once with Rocket Pool
func (m *ConstellationTestManager) RocketPool_RegisterNodes(timezones []string, opts []*bind.TransactOpts) ([]*node.Node, error) {
	rp := m.sp.GetRocketPoolManager().RocketPool
	txMgr := m.sp.GetTransactionManager()
	nodes := make([]*node.Node, len(opts))
	txInfos := make([]*eth.TransactionInfo, len(opts))

	// Make the nodes and TX's
	for i, timezone := range timezones {
		nodeOpts := opts[i]

		// Create the node
		address := nodeOpts.From
		node, err := node.NewNode(rp, address)
		if err != nil {
			return nil, fmt.Errorf("error creating node %s: %w", address.Hex(), err)
		}

		// Register the node
		txInfo, err := node.Register(timezone, nodeOpts)
		if err != nil {
			return nil, fmt.Errorf("error creating node %s registration TX: %w", address.Hex(), err)
		}
		nodes[i] = node
		txInfos[i] = txInfo
	}

	// Submit the TX's
	txs := make([]*types.Transaction, len(txInfos))
	for i, txInfo := range txInfos {
		var err error
		txs[i], err = txMgr.ExecuteTransaction(txInfo, opts[i])
		if err != nil {
			return nil, fmt.Errorf("error submitting node %s registration TX: %w", opts[i].From.Hex(), err)
		}
	}

	// Mine a block
	err := m.CommitBlock()
	if err != nil {
		return nil, fmt.Errorf("error committing block: %w", err)
	}

	// Wait for the TX's
	err = txMgr.WaitForTransactions(txs)
	if err != nil {
		return nil, fmt.Errorf("error waiting for node registration TXs: %w", err)
	}
	return nodes, nil
}

// ==================
// === Oracle DAO ===
// ==================

type OracleDaoNodeCreationDetails struct {
	Opts     *bind.TransactOpts
	Timezone string
	ID       string
	URL      string
}

// Creates a set of Oracle DAO nodes and transact opts with the given mnemonic recovery indices, using the provided key generator and chain ID
func (m *ConstellationTestManager) RocketPool_CreateOracleDaoNodesWithDefaults(keygen *keys.KeyGenerator, chainID *big.Int, indices []uint, owner *bind.TransactOpts) ([]*node.Node, []*bind.TransactOpts, error) {
	keys := make([]*ecdsa.PrivateKey, len(indices))
	opts := make([]*bind.TransactOpts, len(indices))
	for i, index := range indices {
		key, err := keygen.GetEthPrivateKey(index)
		if err != nil {
			return nil, nil, fmt.Errorf("error getting key for index %d: %w", index, err)
		}
		keys[i] = key

		opts[i], err = bind.NewKeyedTransactorWithChainID(key, chainID)
		if err != nil {
			return nil, nil, fmt.Errorf("error creating transactor for index %d: %w", index, err)
		}
	}
	details := make([]OracleDaoNodeCreationDetails, len(indices))
	for i, opt := range opts {
		details[i] = OracleDaoNodeCreationDetails{
			Opts:     opt,
			Timezone: "Etc/UTC",
			ID:       fmt.Sprintf("odao%d", i+1),
			URL:      fmt.Sprintf("https://odao%d.com", i+1),
		}
	}

	nodes, err := m.RocketPool_CreateOracleDaoNodes(details, owner)
	if err != nil {
		return nil, nil, fmt.Errorf("error creating oDAO nodes: %w", err)
	}
	return nodes, opts, nil
}

// Registers a set of nodes and bootstraps them into the Oracle DAO, taking care of all of the details involved
func (m *ConstellationTestManager) RocketPool_CreateOracleDaoNodes(details []OracleDaoNodeCreationDetails, owner *bind.TransactOpts) ([]*node.Node, error) {
	// Get some contract bindings
	rp := m.sp.GetRocketPoolManager().RocketPool
	qMgr := m.sp.GetQueryManager()
	txMgr := m.sp.GetTransactionManager()
	odaoMgr, err := oracle.NewOracleDaoManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error getting oDAO manager binding: %w", err)
	}
	oma, err := rp.GetContract(rocketpool.ContractName_RocketDAONodeTrustedActions)
	if err != nil {
		return nil, fmt.Errorf("error getting OMA contract: %w", err)
	}
	fsrpl, err := tokens.NewTokenRplFixedSupply(rp)
	if err != nil {
		return nil, fmt.Errorf("error getting FSRPL binding: %w", err)
	}
	rpl, err := tokens.NewTokenRpl(rp)
	if err != nil {
		return nil, fmt.Errorf("error getting RPL binding: %w", err)
	}
	rplContract, err := rp.GetContract(rocketpool.ContractName_RocketTokenRPL)
	if err != nil {
		return nil, fmt.Errorf("error getting RPL contract: %w", err)
	}

	// Register the nodes
	timezones := make([]string, len(details))
	opts := make([]*bind.TransactOpts, len(details))
	for i, detail := range details {
		timezones[i] = detail.Timezone
		opts[i] = detail.Opts
	}
	nodes, err := m.RocketPool_RegisterNodes(timezones, opts)
	if err != nil {
		return nil, fmt.Errorf("error registering nodes: %w", err)
	}

	// Get the amount of RPL to mint
	oSettings := odaoMgr.Settings
	err = qMgr.Query(nil, nil, odaoMgr.MemberCount, oSettings.Member.RplBond)
	if err != nil {
		return nil, fmt.Errorf("error getting network info: %w", err)
	}
	rplBond := oSettings.Member.RplBond.Get()

	// Bootstrap and mint RPL for the nodes
	submissions := []*eth.TransactionSubmission{}
	for i, detail := range details {
		address := nodes[i].Address
		nodeSubmissions, err := eth.BatchCreateTransactionSubmissions([]func() (string, *eth.TransactionInfo, error){
			func() (string, *eth.TransactionInfo, error) {
				txInfo, err := odaoMgr.BootstrapMember(detail.ID, detail.URL, address, owner)
				return fmt.Sprintf("bootstrap member %s", address), txInfo, err
			},
			func() (string, *eth.TransactionInfo, error) {
				txInfo, err := m.RocketPool_MintLegacyRpl(address, rplBond, owner)
				return fmt.Sprintf("mint RPL for %s", address), txInfo, err
			},
		}, true)
		if err != nil {
			return nil, err
		}
		submissions = append(submissions, nodeSubmissions...)
	}
	txs, err := txMgr.BatchExecuteTransactions(submissions, &bind.TransactOpts{
		From:      owner.From,
		Signer:    owner.Signer,
		Nonce:     nil,
		Context:   owner.Context,
		GasFeeCap: owner.GasFeeCap,
		GasTipCap: owner.GasTipCap,
	})
	if err != nil {
		return nil, fmt.Errorf("error submitting bootstrap and mint RPL TX submissions: %w", err)
	}

	// Mine the block
	err = m.CommitBlock()
	if err != nil {
		return nil, fmt.Errorf("error committing block: %w", err)
	}
	err = txMgr.WaitForTransactions(txs)
	if err != nil {
		return nil, fmt.Errorf("error waiting for bootstrap and mint RPL TX submissions: %w", err)
	}

	// Swap RPL and Join the oDAO on each node
	txs = []*types.Transaction{}
	for i, node := range nodes {
		nodeOpts := opts[i]
		submissions, err := eth.BatchCreateTransactionSubmissions([]func() (string, *eth.TransactionInfo, error){
			func() (string, *eth.TransactionInfo, error) {
				txInfo, err := fsrpl.Approve(rplContract.Address, rplBond, nodeOpts)
				return fmt.Sprintf("approve RPL usage for %s", node.Address), txInfo, err
			},
			func() (string, *eth.TransactionInfo, error) {
				txInfo, err := rpl.SwapFixedSupplyRplForRpl(rplBond, nodeOpts)
				return fmt.Sprintf("swap RPL for %s", node.Address), txInfo, err
			},
			func() (string, *eth.TransactionInfo, error) {
				txInfo, err := rpl.Approve(oma.Address, rplBond, nodeOpts)
				return fmt.Sprintf("approve oDAO RPL bonding for %s", node.Address), txInfo, err
			},
			func() (string, *eth.TransactionInfo, error) {
				txInfo, err := odaoMgr.Join(nodeOpts)
				return fmt.Sprintf("join oDAO for %s", node.Address), txInfo, err
			},
		}, false)
		if err != nil {
			return nil, err
		}

		// Submit the TX's
		nodeTxs, err := txMgr.BatchExecuteTransactions(submissions, &bind.TransactOpts{
			From:      nodeOpts.From,
			Signer:    nodeOpts.Signer,
			Nonce:     nil,
			Context:   nodeOpts.Context,
			GasFeeCap: nodeOpts.GasFeeCap,
			GasTipCap: nodeOpts.GasTipCap,
		})
		if err != nil {
			return nil, fmt.Errorf("error submitting RPL swap and oDAO join TX submissions for [%s]: %w", node.Address, err)
		}
		txs = append(txs, nodeTxs...)
	}

	// Mine the block
	err = m.CommitBlock()
	if err != nil {
		return nil, fmt.Errorf("error committing block: %w", err)
	}
	err = txMgr.WaitForTransactions(txs)
	if err != nil {
		return nil, fmt.Errorf("error waiting for approve and join oDAO submissions: %w", err)
	}

	return nodes, nil
}

// ==============
// === Tokens ===
// ==============

// Mints legacy RPL and sends it to the specified address
func (m *ConstellationTestManager) RocketPool_MintLegacyRpl(receiver common.Address, amount *big.Int, owner *bind.TransactOpts) (*eth.TransactionInfo, error) {
	rp := m.sp.GetRocketPoolManager().RocketPool
	txMgr := m.sp.GetTransactionManager()
	fsrpl, err := rp.GetContract(rocketpool.ContractName_RocketTokenRPLFixedSupply)
	if err != nil {
		return nil, fmt.Errorf("error creating legacy RPL contract: %w", err)
	}

	return txMgr.CreateTransactionInfo(fsrpl.Contract, "mint", owner, receiver, amount)
}
