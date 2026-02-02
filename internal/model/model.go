package model

type Metric struct {
	Timestamp int64   `json:"timestamp"`
	CPU       float64 `json:"cpu"`
	RPS       float64 `json:"rps"`
	DeviceID  string  `json:"device_id,omitempty"`
}