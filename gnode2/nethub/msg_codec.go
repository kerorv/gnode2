package nethub

// MessageCodec is message codec interface
type MessageCodec interface {
	encode(interface{}) ([]byte, error)
	decode([]byte) (interface{}, error)
}
