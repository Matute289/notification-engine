package dto

type Recipient struct {
	UserID      *int64 `json:"user_id,omitempty"`
	Email       string `json:"email,omitempty"`
	PhoneNumber string `json:"phone_number,omitempty"`
	DeviceToken string `json:"device_token,omitempty"`
}
