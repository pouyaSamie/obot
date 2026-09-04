package types

type AuthProvider struct {
	Metadata
	AuthProviderManifest
	AuthProviderStatus
}

type AuthProviderManifest struct {
	CommonProviderMetadata `json:",inline" yaml:",inline"`
	PostgresTablePrefix    string `json:"postgresTablePrefix,omitempty"`
	GroupIDPrefix          string `json:"groupIDPrefix,omitempty" yaml:"groupIDPrefix,omitempty"`
}

type AuthProviderStatus struct {
	CommonProviderStatus
	Namespace        string `json:"namespace,omitempty"`
	SupportsUserSync bool   `json:"supportsUserSync,omitempty"`
}

type AuthProviderList List[AuthProvider]
