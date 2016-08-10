package pusher

// Event is a pusher event
type Event struct {
	Event string `json:"event"`
	Data  string `json:"data"`
}

// eventError represents a pusher:error data
type eventError struct {
	Message string `json:"message"`
	Code    int    `json:"code"`
}
