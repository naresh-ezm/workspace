package config

import (
	"os"
	"strconv"
)

// Config holds all application configuration loaded from environment variables.
type Config struct {
	Port            string
	DBPath          string
	AWSRegion       string
	LogFile         string
	AdminUsername   string
	AdminPIN        string
	SessionDuration int // hours

	// Workspace provisioning
	WorkspaceAMI             string
	WorkspaceInstanceType    string
	WorkspaceKeyName         string
	WorkspaceSecurityGroupID string
	WorkspaceSubnetID        string
}

// Load reads configuration from environment variables, falling back to defaults.
func Load() *Config {
	return &Config{
		Port:            getEnv("PORT", "8000"),
		DBPath:          getEnv("DB_PATH", "app.db"),
		AWSRegion:       getEnv("AWS_REGION", "us-east-1"),
		LogFile:         getEnv("LOG_FILE", "app.log"),
		AdminUsername:   getEnv("ADMIN_USERNAME", "admin"),
		AdminPIN:        getEnv("ADMIN_PIN", "admin1234"),
		SessionDuration: getEnvInt("SESSION_DURATION_HOURS", 8),

		WorkspaceAMI:             getEnv("WORKSPACE_AMI", ""),
		WorkspaceInstanceType:    getEnv("WORKSPACE_INSTANCE_TYPE", "t3.medium"),
		WorkspaceKeyName:         getEnv("WORKSPACE_KEY_NAME", ""),
		WorkspaceSecurityGroupID: getEnv("WORKSPACE_SECURITY_GROUP_ID", ""),
		WorkspaceSubnetID:        getEnv("WORKSPACE_SUBNET_ID", ""),
	}
}

func getEnv(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if val := os.Getenv(key); val != "" {
		if i, err := strconv.Atoi(val); err == nil {
			return i
		}
	}
	return defaultVal
}
