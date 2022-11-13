package appstore

type Account struct {
	Email               string `json:"email,omitempty"`
	PasswordToken       string `json:"passwordToken,omitempty"`
	DirectoryServicesID string `json:"directoryServicesIdentifier,omitempty"`
	Name                string `json:"name,omitempty"`
}
