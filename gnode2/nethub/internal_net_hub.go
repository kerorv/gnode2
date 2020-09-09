package nethub

// TODO: outputsession应做成msgQ独立，不随着网络连接关闭而关闭。
// outputsession在网络关闭（被动）后，设置最大缓存个数，到达最大阈值后，停止写入，
// 网络恢复后，复用该msgQ，继续发送msgQ中的消息；
// 主动关闭网络连接时，msgQ都应关闭。
import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"gnode2/base"
	"gnode2/gnode2"
)

type codecCreator func() MessageCodec

// InternalHub internal net hub
type InternalHub struct {
	name      string
	listener  net.Listener
	makeCodec codecCreator
	handler   MessageHandler

	inputs map[string]*internalInputSession
	imutex sync.Mutex

	outputs map[string]*internalOutputSession
	omutex  sync.Mutex

	outputMsgQCache sync.Map
}

const (
	connectTimeout     = 10 * time.Second
	reconnectInterval  = 5 * time.Second
	maxOutputThreshold = 1024
	rbuffInitCapacity  = 1024
)

type internalInputSession struct {
	name    string
	conn    net.Conn
	codec   MessageCodec
	rbuffer []byte
	handler MessageHandler
}

type internalOutputSession struct {
	name  string
	conn  net.Conn
	codec MessageCodec
	msgQ  *base.ConcurrentQueue
}

type shakeHandMsg struct {
	from string
}

// NewInternalHub return new net hub
func NewInternalHub(creator codecCreator) *InternalHub {
	hub := &InternalHub{
		makeCodec: creator,
		handler:   gnode2.Gn,
		inputs:    make(map[string]*internalInputSession),
		outputs:   make(map[string]*internalOutputSession),
	}
	return hub
}

// Start start hub
func (hub *InternalHub) Start(address string) bool {
	listener, err := net.Listen("tcp", address)
	if err != nil {
		return false
	}

	hub.name = listener.Addr().String()
	hub.listener = listener

	go hub.accept()
	return true
}

// Stop stop hub
func (hub *InternalHub) Stop(address string) {
	if hub.listener != nil {
		hub.listener.Close()
	}

	func() {
		hub.imutex.Lock()
		defer hub.imutex.Unlock()

		for _, session := range hub.inputs {
			if session.conn != nil {
				session.conn.Close()
			}
		}
	}()

	func() {
		hub.omutex.Lock()
		defer hub.omutex.Unlock()

		for _, session := range hub.outputs {
			if session.msgQ != nil {
				session.msgQ.Close()
			}
		}
	}()
}

func (hub *InternalHub) accept() {
	defer hub.listener.Close()

	for {
		conn, err := hub.listener.Accept()
		if err != nil {
			fmt.Println("[netHub:accept]", err)
			return
		}

		session := &internalInputSession{
			conn:    conn,
			codec:   hub.makeCodec(),
			rbuffer: make([]byte, rbuffInitCapacity),
			handler: hub.handler,
		}

		go session.readLoop(hub)
	}
}

// SendTo send [msg] to [to]
func (hub *InternalHub) SendTo(to string, msg interface{}) {
	session, hasConnection := func() (*internalOutputSession, bool) {
		hub.omutex.Lock()
		defer hub.omutex.Unlock()

		s, ok := hub.outputs[to]
		if !ok {
			s = &internalOutputSession{
				name:  to,
				conn:  nil,
				codec: hub.makeCodec(),
				msgQ:  base.NewConqueue(),
			}
			hub.outputs[to] = s
		}

		if s.msgQ.Len() < maxOutputThreshold {
			s.msgQ.Put(msg)
		}

		return s, s.conn != nil
	}()

	if hasConnection {
		return
	}

	outputLoop := func() {
		for !session.msgQ.IsClose() {
			conn, err := net.DialTimeout("tcp", to, connectTimeout)
			if err != nil {
				fmt.Println(err)
				time.Sleep(reconnectInterval)
				continue
			}

			fmt.Println("connect ", to, " ok")
			func() {
				hub.omutex.Lock()
				defer hub.omutex.Unlock()
				session.conn = conn
			}()

			session.writeLoop(hub)
			time.Sleep(reconnectInterval)
		}
	}

	go outputLoop()
}

func (hub *InternalHub) addInputSession(s *internalInputSession) {
	hub.imutex.Lock()
	defer hub.imutex.Unlock()
	hub.inputs[s.name] = s
}

func (hub *InternalHub) removeInputSession(s *internalInputSession) {
	hub.imutex.Lock()
	defer hub.imutex.Unlock()
	delete(hub.inputs, s.name)
}

func (s *internalInputSession) read() (interface{}, error) {
	_, err := io.ReadFull(s.conn, s.rbuffer[:4])
	if err != nil {
		fmt.Println("[internalInputSession::read]", err)
		return nil, err
	}

	length := binary.LittleEndian.Uint32(s.rbuffer[:4])
	if length == 0 {
		return nil, fmt.Errorf("proto error: length=0")
	}
	if length > uint32(cap(s.rbuffer)) {
		slice := make([]byte, length)
		s.rbuffer = slice
	}

	_, err = io.ReadFull(s.conn, s.rbuffer)
	if err != nil {
		fmt.Println("[internalInputSession::read]", err)
		return nil, err
	}

	msg, err := s.codec.decode(s.rbuffer)
	if err != nil {
		fmt.Println("[internalInputSession::read]", err)
		return nil, err
	}

	return msg, nil
}

func (s *internalOutputSession) write(msg interface{}) error {
	dataBytes, err := s.codec.encode(msg)
	if err != nil {
		fmt.Println("[internalOutputSession::write]", err)
		return err
	}

	var lengthBytes [4]byte
	binary.LittleEndian.PutUint32(lengthBytes[:], uint32(len(dataBytes)))
	if err = writeFull(s.conn, lengthBytes[:]); err != nil {
		fmt.Println("[internalOutputSession::write]", err)
		return err
	}

	if err = writeFull(s.conn, dataBytes); err != nil {
		fmt.Println("[internalOutputSession::write]", err)
		return err
	}

	return nil
}

func writeFull(conn net.Conn, data []byte) error {
	length := len(data)
	for written := 0; written < length; {
		onceWriteBytes, err := conn.Write(data[written:])
		if err != nil {
			return err
		}

		written += onceWriteBytes
	}

	return nil
}

func (s *internalInputSession) shakeHand(hub *InternalHub) bool {
	msg, err := s.read()
	if err != nil {
		return false
	}

	shakeHand, ok := msg.(shakeHandMsg)
	if !ok {
		return false
	}

	s.name = shakeHand.from
	hub.addInputSession(s)
	return true
}

func (s *internalInputSession) readLoop(hub *InternalHub) {
	defer func() {
		s.conn.Close()
		hub.removeInputSession(s)
	}()

	if !s.shakeHand(hub) {
		fmt.Println("shake hand fail")
		return
	}

	for {
		msg, err := s.read()
		if err != nil {
			fmt.Println("read fail", err)
			return
		}

		s.handler.OnMessage(msg)
	}
}

func (s *internalOutputSession) shakeHand(hub *InternalHub) bool {
	msg := shakeHandMsg{hub.name}
	err := s.write(msg)
	return (err == nil)
}

func (s *internalOutputSession) writeLoop(hub *InternalHub) {
	defer func() {
		hub.omutex.Lock()
		defer hub.omutex.Unlock()

		s.conn.Close()
		s.conn = nil
	}()

	if !s.shakeHand(hub) {
		return
	}

	for {
		msg := s.msgQ.Wait()
		if msg == nil {
			return
		}

		if err := s.write(msg); err != nil {
			return
		}
	}
}
