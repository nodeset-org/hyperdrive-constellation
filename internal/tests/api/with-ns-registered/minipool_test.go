package with_ns_registered

import (
	"testing"

	hdtesting "github.com/nodeset-org/hyperdrive-daemon/testing"
	"github.com/stretchr/testify/require"
)

func TestMinipoolGetCount_Zero(t *testing.T) {
	// Take a snapshot, revert at the end
	snapshotName, err := testMgr.CreateCustomSnapshot(hdtesting.Service_EthClients | hdtesting.Service_Filesystem | hdtesting.Service_NodeSet)
	if err != nil {
		fail("Error creating custom snapshot: %v", err)
	}
	defer nodeset_cleanup(snapshotName)

	// Check if the node is registered
	cs := testMgr.GetApiClient()
	countResponse, err := cs.Minipool.GetAvailableMinipoolCount()
	require.NoError(t, err)
	require.Equal(t, 0, countResponse.Data.Count)
}
