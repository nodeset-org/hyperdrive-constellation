package main

import (
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"os/signal"
	"strconv"
	"sync"
	"syscall"

	cscommon "github.com/nodeset-org/hyperdrive-constellation/common"
	"github.com/nodeset-org/hyperdrive-constellation/server"
	csshared "github.com/nodeset-org/hyperdrive-constellation/shared"
	csconfig "github.com/nodeset-org/hyperdrive-constellation/shared/config"
	cstasks "github.com/nodeset-org/hyperdrive-constellation/tasks"
	"github.com/nodeset-org/hyperdrive-daemon/module-utils/services"
	"github.com/nodeset-org/hyperdrive-daemon/shared"
	"github.com/nodeset-org/hyperdrive-daemon/shared/auth"
	"github.com/nodeset-org/hyperdrive-daemon/shared/config"
	hdconfig "github.com/nodeset-org/hyperdrive-daemon/shared/config"
	"github.com/urfave/cli/v2"
)

// Run
func main() {
	// Add logo and attribution to application help template
	attribution := "ATTRIBUTION:\n   Adapted from the Rocket Pool Smart Node (https://github.com/rocketpool/smartnode) with love."
	cli.AppHelpTemplate = fmt.Sprintf("\n%s\n\n%s\n%s\n", shared.Logo, cli.AppHelpTemplate, attribution)
	cli.CommandHelpTemplate = fmt.Sprintf("%s\n%s\n", cli.CommandHelpTemplate, attribution)
	cli.SubcommandHelpTemplate = fmt.Sprintf("%s\n%s\n", cli.SubcommandHelpTemplate, attribution)

	// Initialise application
	app := cli.NewApp()

	// Set application info
	app.Name = "constellation-daemon"
	app.Usage = "Hyperdrive Daemon for NodeSet Constellation Module Management"
	app.Version = csshared.ConstellationVersion
	app.Authors = []*cli.Author{
		{
			Name:  "Nodeset",
			Email: "info@nodeset.io",
		},
	}
	app.Copyright = "(C) 2024 NodeSet LLC"

	moduleDirFlag := &cli.StringFlag{
		Name:     "module-dir",
		Aliases:  []string{"d"},
		Usage:    "The path to the Constellation module data directory",
		Required: true,
	}
	hyperdriveUrlFlag := &cli.StringFlag{
		Name:    "hyperdrive-url",
		Aliases: []string{"hd"},
		Usage:   "The URL of the Hyperdrive API",
		Value:   "http://127.0.0.1:" + strconv.FormatUint(uint64(config.DefaultApiPort), 10),
	}
	settingsFolderFlag := &cli.StringFlag{
		Name:     "settings-folder",
		Aliases:  []string{"s"},
		Usage:    "The path to the folder containing the network settings files",
		Required: true,
	}
	ipFlag := &cli.StringFlag{
		Name:    "ip",
		Aliases: []string{"i"},
		Usage:   "The IP address to bind the API server to",
		Value:   "127.0.0.1",
	}
	portFlag := &cli.UintFlag{
		Name:    "port",
		Aliases: []string{"p"},
		Usage:   "The port to bind the API server to",
		Value:   uint(csconfig.DefaultApiPort),
	}
	apiKeyFlag := &cli.StringFlag{
		Name:     "api-key",
		Aliases:  []string{"k"},
		Usage:    "Path of the key to use for authenticating incoming API requests",
		Required: true,
	}
	hyperdriveApiKeyFlag := &cli.StringFlag{
		Name:     "hd-api-key",
		Aliases:  []string{"hk"},
		Usage:    "Path of the key to use when sending requests to the Hyperdrive API",
		Required: true,
	}

	app.Flags = []cli.Flag{
		moduleDirFlag,
		hyperdriveUrlFlag,
		settingsFolderFlag,
		ipFlag,
		portFlag,
		apiKeyFlag,
		hyperdriveApiKeyFlag,
	}
	app.Action = func(c *cli.Context) error {
		// Get the env vars
		moduleDir := c.String(moduleDirFlag.Name)
		hdUrlString := c.String(hyperdriveUrlFlag.Name)
		hyperdriveUrl, err := url.Parse(hdUrlString)
		if err != nil {
			return fmt.Errorf("error parsing Hyperdrive URL [%s]: %w", hdUrlString, err)
		}

		// Get the settings file path
		settingsFolder := c.String(settingsFolderFlag.Name)
		if settingsFolder == "" {
			fmt.Println("No settings folder provided.")
			os.Exit(1)
		}
		_, err = os.Stat(settingsFolder)
		if errors.Is(err, fs.ErrNotExist) {
			fmt.Printf("Settings folder not found at [%s].", settingsFolder)
			os.Exit(1)
		}

		// Load the network settings
		settingsList, err := csconfig.LoadSettingsFiles(settingsFolder)
		if err != nil {
			fmt.Printf("Error loading network settings: %s", err)
			os.Exit(1)
		}

		// Make an incoming API auth manager
		apiKeyPath := c.String(apiKeyFlag.Name)
		moduleAuthMgr := auth.NewAuthorizationManager(apiKeyPath, "cs-daemon-svr", auth.DefaultRequestLifespan)
		err = moduleAuthMgr.LoadAuthKey()
		if err != nil {
			return fmt.Errorf("error loading module API key: %w", err)
		}

		// Make an HD API auth manager
		hdApiKeyPath := c.String(hyperdriveApiKeyFlag.Name)
		hdAuthMgr := auth.NewAuthorizationManager(hdApiKeyPath, "cs-daemon-client", auth.DefaultRequestLifespan)
		err = hdAuthMgr.LoadAuthKey()
		if err != nil {
			return fmt.Errorf("error loading Hyperdrive API key: %w", err)
		}

		// Wait group to handle the API server (separate because of error handling)
		stopWg := new(sync.WaitGroup)

		// Create the service provider
		configFactory := func(hdCfg *hdconfig.HyperdriveConfig) (*csconfig.ConstellationConfig, error) {
			return csconfig.NewConstellationConfig(hdCfg, settingsList)
		}
		sp, err := services.NewModuleServiceProvider(hyperdriveUrl, moduleDir, csconfig.ModuleName, csconfig.ClientLogName, configFactory, hdAuthMgr)
		if err != nil {
			return fmt.Errorf("error creating service provider: %w", err)
		}
		constellationSp, err := cscommon.NewConstellationServiceProvider(sp, settingsList)
		if err != nil {
			return fmt.Errorf("error creating Constellation service provider: %w", err)
		}

		// Start the task loop
		fmt.Println("Starting task loop...")
		taskLoop := cstasks.NewTaskLoop(constellationSp, stopWg)
		err = taskLoop.Run()
		if err != nil {
			return fmt.Errorf("error starting task loop: %w", err)
		}

		// Start the server after the task loop so it can log into NodeSet before this starts serving registration status checks
		ip := c.String(ipFlag.Name)
		port := c.Uint64(portFlag.Name)
		serverMgr, err := server.NewServerManager(constellationSp, ip, uint16(port), stopWg, moduleAuthMgr)
		if err != nil {
			return fmt.Errorf("error creating Constellation server: %w", err)
		}

		// Handle process closures
		termListener := make(chan os.Signal, 1)
		signal.Notify(termListener, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-termListener
			fmt.Println("Shutting down daemon...")
			constellationSp.CancelContextOnShutdown()
			serverMgr.Stop()
		}()

		// Run the daemon until closed
		fmt.Println("Daemon online.")
		fmt.Printf("HD client calls are being logged to: %s\n", sp.GetClientLogger().GetFilePath())
		fmt.Printf("API calls are being logged to: %s\n", sp.GetApiLogger().GetFilePath())
		fmt.Printf("Tasks are being logged to:     %s\n", sp.GetTasksLogger().GetFilePath())
		fmt.Println("To view them, use `hyperdrive service daemon-logs [cs-hd | cs-api | cs-tasks].") // TODO: don't hardcode
		stopWg.Wait()
		sp.Close()
		fmt.Println("Daemon stopped.")
		return nil
	}

	// Run application
	if err := app.Run(os.Args); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
