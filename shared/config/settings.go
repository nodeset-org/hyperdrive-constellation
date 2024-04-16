package csconfig

const (
	ModuleName           string = "constellation"
	ShortModuleName      string = "cs"
	DaemonBaseRoute      string = ModuleName
	ApiVersion           string = "1"
	ApiClientRoute       string = DaemonBaseRoute + "/api/v" + ApiVersion
	CliSocketFilename    string = ModuleName + "-cli.sock"
	NetSocketFilename    string = ModuleName + "-net.sock"
	WalletFilename       string = "wallet.json"
	PasswordFilename     string = "password.txt"
	KeystorePasswordFile string = "secret.txt"
	DepositDataFile      string = "deposit-data.json"
	DefaultApiPort       uint16 = 8181

	// Logging
	ClientLogName string = "hd.log"
)
