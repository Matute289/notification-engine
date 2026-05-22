package dto

type SettingRequest struct {
	Channel string `json:"channel"`
	OptIn   bool   `json:"opt_in"`
}
