package nethub

import (
	"fmt"

	"google.golang.org/protobuf/proto"
)

// ProtomsgCodec is a protobuf message codec
type ProtomsgCodec struct {
}

func (c *ProtomsgCodec) encode(msg interface{}) ([]byte, error) {
	pbmsg, ok := msg.(*proto.Message)
	if ok {
		fmt.Println(pbmsg)
	}
	return nil, nil
}

func (c *ProtomsgCodec) decode([]byte) (interface{}, error) {
	return nil, nil
}

// CreateProtomsgCodec create protobuff message codec
func CreateProtomsgCodec() MessageCodec {
	codec := &ProtomsgCodec{}
	return codec
}
