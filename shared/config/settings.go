package csconfig

const (
	ModuleName           string = "constellation"
	ShortModuleName      string = "cs"
	DaemonBaseRoute      string = ModuleName
	ApiVersion           string = "1"
	ApiClientRoute       string = DaemonBaseRoute + "/api/v" + ApiVersion
	DefaultApiPort       uint16 = 8280
	DefaultVcMetricsPort uint16 = 9111
	KeystorePasswordFile string = "secret.txt"

	// Logging
	ClientLogName string = "hd.log"
)
