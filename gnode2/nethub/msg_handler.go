package nethub

// MessageHandler is message handler interface
type MessageHandler interface {
	OnMessage(interface{})
}
