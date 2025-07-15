package types

// ClientConfig represents a client configuration with all necessary settings
type ClientConfig struct {
	Name       string            `yaml:"name" json:"name"`
	URL        string            `yaml:"url" json:"url"`
	Headers    map[string]string `yaml:"headers,omitempty" json:"headers,omitempty"`
	Timeout    string            `yaml:"timeout,omitempty" json:"timeout,omitempty"`
	MaxRetries int               `yaml:"max_retries,omitempty" json:"max_retries,omitempty"`
	RateLimit  *RateLimitConfig  `yaml:"rate_limit,omitempty" json:"rate_limit,omitempty"`
	Auth       *AuthConfig       `yaml:"auth,omitempty" json:"auth,omitempty"`
}

// RateLimitConfig defines rate limiting settings for a client
type RateLimitConfig struct {
	RequestsPerSecond int `yaml:"requests_per_second" json:"requests_per_second"`
	Burst             int `yaml:"burst,omitempty" json:"burst,omitempty"`
}

// AuthConfig defines authentication settings for a client
type AuthConfig struct {
	Type     string `yaml:"type" json:"type"` // "basic", "bearer", "api_key"
	Username string `yaml:"username,omitempty" json:"username,omitempty"`
	Password string `yaml:"password,omitempty" json:"password,omitempty"`
	Token    string `yaml:"token,omitempty" json:"token,omitempty"`
	APIKey   string `yaml:"api_key,omitempty" json:"api_key,omitempty"`
}

// ClientsConfig represents a collection of client configurations
type ClientsConfig struct {
	Clients []ClientConfig `yaml:"clients"`
}
