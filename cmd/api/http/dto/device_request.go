package dto

type DeviceRequest struct {
	DeviceToken string `json:"device_token"`
	Channel     string `json:"channel"`
}
