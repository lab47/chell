package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/hashicorp/go-hclog"
	"github.com/lab47/chell/pkg/config"
	"github.com/lab47/chell/pkg/ops"
	homedir "github.com/mitchellh/go-homedir"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

var (
	// Used for flags.
	cfgFile     string
	userLicense string

	rootCmd = &cobra.Command{
		Use:   "chell",
		Short: "A software management tool",
		Long:  ``,
	}
)

var (
	debug int
)

// Execute executes the root command.
func Execute() error {
	return rootCmd.Execute()
}

func loadAPI() (*ops.Ops, *config.Config, error) {
	cfg, err := config.LoadConfig()
	if err != nil {
		return nil, nil, err
	}

	level := hclog.Info

	switch debug {
	case 0:
		// ok
	case 1:
		level = hclog.Debug
	default:
		level = hclog.Trace
	}

	logger := hclog.New(&hclog.LoggerOptions{
		Name:            "chell",
		IncludeLocation: true,
		Level:           level,
	})

	o, err := ops.NewOps(logger, cfg)
	if err != nil {
		return nil, nil, err
	}

	return o, cfg, nil
}

func parseName(name string) (string, string) {
	idx := strings.LastIndexByte(name, '.')
	if idx == -1 {
		return "", name
	}

	return name[:idx], name[idx+1:]
}

func init() {
	cobra.OnInitialize(initConfig)

	rootCmd.PersistentFlags().CountVarP(&debug, "debug", "D", "debug level")

	// rootCmd.PersistentFlags().StringVar(&cfgFile, "config", "", "config file (default is $HOME/.cobra.yaml)")
	// rootCmd.PersistentFlags().StringP("author", "a", "YOUR NAME", "author name for copyright attribution")
	// rootCmd.PersistentFlags().StringVarP(&userLicense, "license", "l", "", "name of license for the project")
	// rootCmd.PersistentFlags().Bool("viper", true, "use Viper for configuration")
	// viper.BindPFlag("author", rootCmd.PersistentFlags().Lookup("author"))
	// viper.BindPFlag("useViper", rootCmd.PersistentFlags().Lookup("viper"))
	// viper.SetDefault("author", "NAME HERE <EMAIL ADDRESS>")
	// viper.SetDefault("license", "apache")

	rootCmd.AddCommand(installCmd)
	rootCmd.AddCommand(sumCmd)
	rootCmd.AddCommand(inspectCmd)
	rootCmd.AddCommand(shellCmd)
	rootCmd.AddCommand(gcCmd)
	rootCmd.AddCommand(removeCmd)
	rootCmd.AddCommand(exportKeyCmd)
	rootCmd.AddCommand(calcCmd)
	rootCmd.AddCommand(openCmd)
	rootCmd.AddCommand(buildCmd)
	rootCmd.AddCommand(uploadCmd)
}

func er(msg interface{}) {
	fmt.Println("Error:", msg)
	os.Exit(1)
}

func initConfig() {
	if cfgFile != "" {
		// Use config file from the flag.
		viper.SetConfigFile(cfgFile)
	} else {
		// Find home directory.
		home, err := homedir.Dir()
		if err != nil {
			er(err)
		}

		// Search config in home directory with name ".cobra" (without extension).
		viper.AddConfigPath(home)
		viper.SetConfigName(".cobra")
	}

	viper.AutomaticEnv()

	if err := viper.ReadInConfig(); err == nil {
		fmt.Println("Using config file:", viper.ConfigFileUsed())
	}
}
