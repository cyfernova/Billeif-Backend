package main

import (
	"fmt"

	"github.com/spf13/viper"
)

type DatabaseConfig struct {
	Host     string `mapstructure:"HOST"`
	Port     int    `mapstructure:"PORT"`
	User     string `mapstructure:"USER"`
	Password string `mapstructure:"PASSWORD"`
	Name     string `mapstructure:"NAME"`
	SSLMode  string `mapstructure:"SSL_MODE"`
}

type Config struct {
	Database DatabaseConfig `mapstructure:"DATABASE"`
}

func main() {
	viper.SetConfigType("env")
	viper.AddConfigPath(".")
	viper.AddConfigPath("..")
	viper.AutomaticEnv()

	viper.SetConfigName(".env.test")
	if err := viper.ReadInConfig(); err != nil {
		panic(fmt.Sprintf("Failed to read .env: %v", err))
	}

	fmt.Println("All keys in Viper:")
	for _, key := range viper.AllKeys() {
		fmt.Printf("  %s = '%s'\n", key, viper.GetString(key))
	}

	var cfg Config
	if err := viper.Unmarshal(&cfg); err != nil {
		panic(fmt.Sprintf("Failed to unmarshal: %v", err))
	}

	fmt.Printf("\nDatabase Host: '%s'\n", cfg.Database.Host)
	fmt.Printf("Database Port: '%d'\n", cfg.Database.Port)
	fmt.Printf("Database User: '%s'\n", cfg.Database.User)
	fmt.Printf("Database Password: '%s'\n", cfg.Database.Password)
	fmt.Printf("Database Name: '%s'\n", cfg.Database.Name)
	fmt.Printf("Database SSLMode: '%s'\n", cfg.Database.SSLMode)

	if cfg.Database.Host == "" {
		fmt.Println("\nDATABASE_HOST is empty after Unmarshal!")
	} else {
		fmt.Println("\nSuccessfully loaded database config!")
	}
}
