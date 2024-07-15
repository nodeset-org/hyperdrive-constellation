package csapi

import (
	snapi "github.com/rocket-pool/smartnode/v2/shared/types/api"
)

type MinipoolCloseDetailsData struct {
	Details []snapi.MinipoolCloseDetails `json:"details"`
}

type MinipoolGetAvailableMinipoolCount struct {
	Count int `json:"count"`
}

type MinipoolDepositMinipool struct {
	Signature string `json:"signature"`
}
