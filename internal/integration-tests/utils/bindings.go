package cstestutils

import (
	"fmt"

	"github.com/ethereum/go-ethereum/common"
	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/nodeset-org/hyperdrive-constellation/common/contracts"
	batch "github.com/rocket-pool/batch-query"
	"github.com/rocket-pool/rocketpool-go/v2/core"
	"github.com/rocket-pool/rocketpool-go/v2/dao/oracle"
	"github.com/rocket-pool/rocketpool-go/v2/dao/protocol"
	"github.com/rocket-pool/rocketpool-go/v2/deposit"
	"github.com/rocket-pool/rocketpool-go/v2/minipool"
	"github.com/rocket-pool/rocketpool-go/v2/network"
	"github.com/rocket-pool/rocketpool-go/v2/node"
	"github.com/rocket-pool/rocketpool-go/v2/rewards"
	"github.com/rocket-pool/rocketpool-go/v2/rocketpool"
	"github.com/rocket-pool/rocketpool-go/v2/tokens"
)

// Common contract bindings that are used across tests
type ContractBindings struct {
	// Rocket Pool bindings
	DepositPoolManager *deposit.DepositPoolManager
	Rpl                *tokens.TokenRpl
	ProtocolDaoManager *protocol.ProtocolDaoManager
	OracleDaoManager   *oracle.OracleDaoManager
	MinipoolManager    *minipool.MinipoolManager
	NetworkManager     *network.NetworkManager
	NodeManager        *node.NodeManager
	RocketVault        *core.Contract
	RewardsPool        *rewards.RewardsPool
	SmoothingPool      *core.Contract

	// Constellation bindings
	Weth                                     *contracts.Weth
	RpSuperNode                              *node.Node
	NodeSetOperatorRewardsDistributorAddress common.Address
}

// Create a new contract bindings instance
func CreateBindings(sp cscommon.IConstellationServiceProvider) (*ContractBindings, error) {
	// Services
	rp := sp.GetRocketPoolManager().RocketPool
	csMgr := sp.GetConstellationManager()
	ec := sp.GetEthClient()
	qMgr := sp.GetQueryManager()
	txMgr := sp.GetTransactionManager()

	// Rocket Pool
	dpMgr, err := deposit.NewDepositPoolManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating deposit pool manager binding: %w", err)
	}
	rpl, err := tokens.NewTokenRpl(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating RPL token binding: %w", err)
	}
	pdaoMgr, err := protocol.NewProtocolDaoManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating protocol DAO manager binding: %w", err)
	}
	odaoMgr, err := oracle.NewOracleDaoManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating oracle DAO manager binding: %w", err)
	}
	mpMgr, err := minipool.NewMinipoolManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating minipool manager binding: %w", err)
	}
	netMgr, err := network.NewNetworkManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating network manager binding: %w", err)
	}
	nodeMgr, err := node.NewNodeManager(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating node manager binding: %w", err)
	}
	rocketVault, err := rp.GetContract(rocketpool.ContractName_RocketVault)
	if err != nil {
		return nil, fmt.Errorf("error getting Rocket Vault contract: %w", err)
	}
	rewardsPool, err := rewards.NewRewardsPool(rp)
	if err != nil {
		return nil, fmt.Errorf("error creating rewards pool binding: %w", err)
	}
	smoothingPool, err := rp.GetContract(rocketpool.ContractName_RocketSmoothingPool)
	if err != nil {
		return nil, fmt.Errorf("error creating smoothin pool contract: %w", err)
	}

	// Constellation
	supernodeAddress := csMgr.SuperNodeAccount.Address
	var wethAddress common.Address
	var nodeSetOperatorRewardsDistributorAddress common.Address
	err = qMgr.Query(func(mc *batch.MultiCaller) error {
		csMgr.Directory.GetWethAddress(mc, &wethAddress)
		csMgr.Directory.GetOperatorRewardAddress(mc, &nodeSetOperatorRewardsDistributorAddress)
		return nil
	}, nil)
	if err != nil {
		return nil, fmt.Errorf("error querying Constellation contract addresses: %w", err)
	}
	rpSuperNode, err := node.NewNode(rp, supernodeAddress)
	if err != nil {
		return nil, fmt.Errorf("error creating RP supernode binding: %w", err)
	}
	weth, err := contracts.NewWeth(wethAddress, ec, qMgr, txMgr, nil)
	if err != nil {
		return nil, fmt.Errorf("error creating WETH binding: %w", err)
	}
	return &ContractBindings{
		// Rocket Pool
		DepositPoolManager: dpMgr,
		Rpl:                rpl,
		ProtocolDaoManager: pdaoMgr,
		OracleDaoManager:   odaoMgr,
		MinipoolManager:    mpMgr,
		NetworkManager:     netMgr,
		NodeManager:        nodeMgr,
		RocketVault:        rocketVault,
		RewardsPool:        rewardsPool,
		SmoothingPool:      smoothingPool,

		// Constellation
		Weth:                                     weth,
		RpSuperNode:                              rpSuperNode,
		NodeSetOperatorRewardsDistributorAddress: nodeSetOperatorRewardsDistributorAddress,
	}, nil
}
