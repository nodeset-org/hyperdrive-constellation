package csapi

type MinipoolGetAvailableMinipoolCount struct {
	Count int `json:"count"`
}

type MinipoolDepositMinipool struct {
	Signature string `json:"signature"`
}
