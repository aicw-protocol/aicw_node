package config

import (
	"fmt"
	"log"
	"strings"

	mpciumconfig "github.com/fystack/mpcium/pkg/config"
	"github.com/spf13/viper"
)

// InitViperConfigMerged loads network-wide defaults first, then merges operator-local
// overrides on top. Operator values win on conflict. If operatorPath is empty, only
// networkPath is loaded (still valid when badger_password comes from -f password.txt).
func InitViperConfigMerged(networkPath, operatorPath string) error {
	if networkPath == "" {
		return fmt.Errorf("network config path is required")
	}

	viper.Reset()
	viper.SetConfigType("yaml")
	viper.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	viper.AutomaticEnv()

	viper.SetConfigFile(networkPath)
	if err := viper.ReadInConfig(); err != nil {
		return fmt.Errorf("read network config %q: %w", networkPath, err)
	}
	log.Printf("Reading network config: %s", networkPath)

	if operatorPath != "" {
		viper.SetConfigFile(operatorPath)
		if err := viper.MergeInConfig(); err != nil {
			return fmt.Errorf("merge operator config %q: %w", operatorPath, err)
		}
		log.Printf("Merged operator config: %s", operatorPath)
	}

	log.Println("Initialized config successfully!")
	return nil
}

// InitViperConfigSingle loads a single config file (backward compatible with --config only).
func InitViperConfigSingle(configPath string) {
	mpciumconfig.InitViperConfig(configPath)
}
