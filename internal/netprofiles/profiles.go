package netprofiles

// NetworkProfile defines the endpoints and configuration for a specific Accumulate network
type NetworkProfile struct {
	JSONRPC []string `json:"jsonrpc"`
	WSPath  string   `json:"ws_path"`
}

// Profiles contains the predefined network configurations for Accumulate networks
var Profiles = map[string]NetworkProfile{
	"devnet": {
		JSONRPC: []string{"http://127.0.0.1:26660"},
		WSPath:  "/ws",
	},
	"testnet": {
		JSONRPC: []string{"https://testnet.accumulate.net/v3"},
		WSPath:  "/ws",
	},
	"mainnet": {
		JSONRPC: []string{"https://mainnet.accumulate.net/v3"},
		WSPath:  "/ws",
	},
}

// GetProfile returns the network profile for the given name
func GetProfile(name string) (NetworkProfile, bool) {
	profile, exists := Profiles[name]
	return profile, exists
}

// GetAvailableNetworks returns a list of available network names
func GetAvailableNetworks() []string {
	networks := make([]string, 0, len(Profiles))
	for name := range Profiles {
		networks = append(networks, name)
	}
	return networks
}

// IsValidNetwork checks if the given network name is valid
func IsValidNetwork(name string) bool {
	_, exists := Profiles[name]
	return exists
}