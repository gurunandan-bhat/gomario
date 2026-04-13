package config

import (
	"fmt"
	"os"

	"github.com/spf13/viper"
)

const (
	defaultConfigFileName = ".gomario.json"
)

type Config struct {
	IsProduction bool   `mapstructure:"isProduction"`
	AppRoot      string `mapstructure:"appRoot"`
	HttpHost     string `mapstructure:"httpHost"`
	HttpPort     int    `mapstructure:"httpPort"`
	Cookie       struct {
		SecretKey string `mapstructure:"secretKey"`
	} `mapstructure:"cookie"`
	Db struct {
		Address              string `mapstructure:"address"`
		Net                  string `mapstructure:"net"`
		DbName               string `mapstructure:"dbName"`
		User                 string `mapstructure:"user"`
		Password             string `mapstructure:"password"`
		ParseTime            bool   `mapstructure:"parseTime"`
		Location             string `mapstructure:"location"`
		AllowNativePasswords bool   `mapstructure:"allowNativePasswords"`
	} `mapstructure:"db"`
	Notifications struct {
		Email string `mapstructure:"email"`
	} `mapstructure:"notifications"`
	Session struct {
		CookieName string `mapstructure:"cookieName"`
	} `mapstructure:"session"`
	Smtp struct {
		Host     string `mapstructure:"host"`
		Port     int    `mapstructure:"port"`
		Username string `mapstructure:"username"`
		Password string `mapstructure:"password"`
		From     string `mapstructure:"from"`
	} `mapstructure:"smtp"`
	Telemetry struct {
		Enabled     bool   `mapstructure:"enabled"`
		ServiceName string `mapstructure:"serviceName"`
		Endpoint    string `mapstructure:"endpoint"` // OTLP HTTP endpoint, e.g. "http://localhost:4318"
	} `mapstructure:"telemetry"`
	Cognito struct {
		Region       string `mapstructure:"region"`
		UserPoolID   string `mapstructure:"userPoolId"`
		ClientID     string `mapstructure:"clientId"`
		ClientSecret string `mapstructure:"clientSecret"`
		Domain       string `mapstructure:"domain"`      // Hosted UI domain, e.g. "auth.example.com" (no https://)
		CallbackURL  string `mapstructure:"callbackUrl"` // e.g. "https://example.com/auth/callback"
		LogoutURL    string `mapstructure:"logoutUrl"`   // e.g. "https://example.com"
	} `mapstructure:"cognito"`
}

var c = Config{}

func Configuration(configFileName ...string) (*Config, error) {

	if (c == Config{}) {

		var cfName string
		switch len(configFileName) {
		case 0:
			dirname, err := os.UserHomeDir()
			if err != nil {
				return nil, err
			}
			cfName = fmt.Sprintf("%s/%s", dirname, defaultConfigFileName)
		case 1:
			cfName = configFileName[0]
		default:
			return nil, fmt.Errorf("incorrect arguments for configuration file name")
		}

		viper.SetConfigFile(cfName)
		if err := viper.ReadInConfig(); err != nil {
			return nil, err
		}

		if err := viper.Unmarshal(&c); err != nil {
			return nil, err
		}
	}

	return &c, nil
}
