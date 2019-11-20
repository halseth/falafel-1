package main

import "text/template"

type headerParams struct {
	ToolName  string
	FileName  string
	Package   string
	TargetPkg string
	BuildTags string
}

var headerTemplate = template.Must(template.New("header").Parse(`// Code generated by {{.ToolName}}. DO NOT EDIT.
// source: {{.FileName}}
{{if .BuildTags}}
{{.BuildTags}}
{{end}}
package {{.Package}}

import (
	"context"
	"net"
	"time"

	"google.golang.org/grpc"

	"github.com/golang/protobuf/proto"

	"{{.TargetPkg}}"
)
`))

type serviceParams struct {
	ServiceName string
	TargetName  string
	Listener    string
}

var serviceTemplate = template.Must(template.New("service").Parse(`
func get{{.ServiceName}}Conn() (*grpc.ClientConn, func(), error) {
	conn, err := {{.Listener}}.Dial()
	if err != nil {
		return nil, nil, err
	}

	clientConn, err := grpc.Dial("",
		grpc.WithDialer(func(target string,
			timeout time.Duration) (net.Conn, error) {
			return conn, nil
		}),
		grpc.WithInsecure(),
		grpc.WithBackoffMaxDelay(10*time.Second),
	)
	if err != nil {
		conn.Close()
		return nil, nil, err
	}

	closeConn := func() {
		conn.Close()
	}

	return clientConn, closeConn, nil
}

// get{{.ServiceName}}Client returns a client connection to the server listening
// on lis.
func get{{.ServiceName}}Client() ({{.TargetName}}.{{.ServiceName}}Client, func(), error) {
	clientConn, closeConn, err := get{{.ServiceName}}Conn()
	if err != nil {
		return nil, nil, err
	}
	client := {{.TargetName}}.New{{.ServiceName}}Client(clientConn)
	return client, closeConn, nil
}
`))

type rpcParams struct {
	ServiceName string
	MethodName  string
	RequestType string
	Comment     string
	ApiPrefix   string
}

var (
	syncTemplate = template.Must(template.New("sync").Parse(`
{{.Comment}}
//
// NOTE: This method produces a single result or error, and the callback will
// be called only once.
func {{.ApiPrefix}}{{.MethodName}}(msg []byte, callback Callback) {
	s := &syncHandler{
		newProto: func() proto.Message {
			return &{{.RequestType}}{}
		},
		getSync: func(ctx context.Context,
			req proto.Message) (proto.Message, error) {

			// Get the gRPC client.
			client, closeClient, err := get{{.ServiceName}}Client()
			if err != nil {
				return nil, err
			}
			defer closeClient()

			r := req.(*{{.RequestType}})
			return client.{{.MethodName}}(ctx, r)
		},
	}
	s.start(msg, callback)
}
`))

	readStreamTemplate = template.Must(template.New("readStream").Parse(`
{{.Comment}}
//
// NOTE: This method produces a stream of responses, and the receive stream can
// be called zero or more times. After EOF error is returned, no more responses
// will be produced.
func {{.ApiPrefix}}{{.MethodName}}(msg []byte, rStream RecvStream) {
	s := &readStreamHandler{
		newProto: func() proto.Message {
			return &{{.RequestType}}{}
		},
		recvStream: func(ctx context.Context,
			req proto.Message) (*receiver, func(), error) {

			// Get the gRPC client.
			client, closeClient, err := get{{.ServiceName}}Client()
			if err != nil {
				return nil, nil, err
			}

			r := req.(*{{.RequestType}})
			stream, err := client.{{.MethodName}}(ctx, r)
			if err != nil {
				closeClient()
				return nil, nil, err
			}
			return &receiver{
				recv: func() (proto.Message, error) {
					return stream.Recv()
				},
			}, closeClient, nil
		},
	}
	s.start(msg, rStream)
}
`))

	biStreamTemplate = template.Must(template.New("biStream").Parse(`
{{.Comment}}
//
// NOTE: This method produces a stream of responses, and the receive stream can
// be called zero or more times. After EOF error is returned, no more responses
// will be produced. The send stream can accept zero or more requests before it
// is closed.
func {{.ApiPrefix}}{{.MethodName}}(rStream RecvStream) (SendStream, error) {
	b := &biStreamHandler{
		newProto: func() proto.Message {
			return &{{.RequestType}}{}
		},
		biStream: func(ctx context.Context) (*receiver, *sender, func(), error) {

			// Get the gRPC client.
			client, closeClient, err := get{{.ServiceName}}Client()
			if err != nil {
				return nil, nil, nil, err
			}

			stream, err := client.{{.MethodName}}(ctx)
			if err != nil {
				closeClient()
				return nil, nil, nil, err
			}
			return &receiver{
					recv: func() (proto.Message, error) {
						return stream.Recv()
					},
				},
				&sender{
					send: func(req proto.Message) error {
						r := req.(*{{.RequestType}})
						return stream.Send(r)
					},
					closeStream: stream.CloseSend,
				}, closeClient, nil
		},
	}
	return b.start(rStream)
}
`))
)

type memRpcParams struct {
	ToolName  string
	Package   string
	Listeners []string
}

var memRpcTemplate = template.Must(template.New("mem").Parse(`// Code generated by {{.ToolName}} DO NOT EDIT.
package {{.Package}}

import (
	"context"

	"github.com/golang/protobuf/proto"
	"google.golang.org/grpc/test/bufconn"
)
{{range $lis := .Listeners}}
// {{$lis}} is a global in-memory buffer that listeners that is referenced by
// the generated mobile APIs, such that all client calls will be going through
// it.
var {{$lis}} = bufconn.Listen(100)
{{end}}
// Callback is an interface that is passed in by callers of the library, and
// specifies where the responses should be delivered.
type Callback interface {
	// OnResponse is called by the library when a response from the daemon
	// for the associated RPC call is received. The reponse is a serialized
	// protobuf for the expected response, and must be deserialized by the
	// caller.
	OnResponse([]byte)

	// OnError is called by the library if any error is encountered during
	// the execution of the RPC call.
	OnError(error)
}

// RecvStream is an interface that is passed in by callers of the library, and
// specifies where the streaming responses should be delivered.
type RecvStream interface {
	// OnResponse is called by the library when a new stream response from
	// the daemon for the associated RPC call is available. The reponse is
	// a serialized protobuf for the expected response, and must be
	// deserialized by the caller.
	OnResponse([]byte)

	// OnError is called by the library if any error is encountered during
	// the execution of the RPC call, or if the response stream ends. No
	// more stream responses will be received after this.
	OnError(error)
}

// SendStream is an interface that the caller of the library can use to send
// requests to the server during the execution of a bidirectional streaming RPC
// call, or stop the stream.
type SendStream interface {
	// Send sends the serialized protobuf request to the server.
	Send([]byte) error

	// Stop closes the bidirecrional connection.
	Stop() error
}

// sendStream is an internal struct that satisifies the SendStream interface.
// We use it to wrap customizable send and stop methods, that can be tuned to
// the specific RPC call in question.
type sendStream struct {
	send func([]byte) error
	stop func() error
}

// Send sends the serialized protobuf request to the server.
//
// Part of the SendStream interface.
func (s *sendStream) Send(req []byte) error {
	return s.send(req)
}

// Stop closes the bidirectional connection.
//
// Part of the SendStream interface.
func (r *sendStream) Stop() error {
	return r.stop()
}

// receiver is a struct used to hold a generic recv closure, that can be set to
// return messages from the desired stream of responses.
type receiver struct {
	// recv returns a message from the stream of responses.
	recv func() (proto.Message, error)
}

// sender is a struct used to hold a generic send closure, that can be set to
// send messages to the desired stream of requests.
type sender struct {
	// send sends the given message to the request stream.
	send func(proto.Message) error

	// closeStream closes the request stream.
	closeStream func() error
}

// syncHandler is a struct used to call the daemon's RPC interface on methods
// where only one request and one response is expected.
type syncHandler struct {
	// newProto returns an empty struct for the desired grpc request.
	newProto func() proto.Message

	// getSync calls the desired method on the given client in a
	// blocking matter.
	getSync func(context.Context, proto.Message) (proto.Message, error)
}

// start executes the RPC call specified by this syncHandler using the
// specified serialized msg request.
func (s *syncHandler) start(msg []byte, callback Callback) {
	// We must make a copy of the passed byte slice, as there is no
	// guarantee the contents won't be changed while the go routine is
	// executing.
	data := make([]byte, len(msg))
	copy(data[:], msg[:])

	go func() {
		// Get an empty proto of the desired type, and deserialize msg
		// as this proto type.
		req := s.newProto()
		err := proto.Unmarshal(data, req)
		if err != nil {
			callback.OnError(err)
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Now execute the RPC call.
		resp, err := s.getSync(ctx, req)
		if err != nil {
			callback.OnError(err)
			return
		}

		// We serialize the response before returning it to the caller.
		b, err := proto.Marshal(resp)
		if err != nil {
			callback.OnError(err)
			return
		}

		callback.OnResponse(b)
	}()
}

// readStreamHandler is a struct used to call the daemon's RPC interface on
// methods where a stream of responses is expected, as in subscription type
// requests.
type readStreamHandler struct {
	// newProto returns an empty struct for the desired grpc request.
	newProto func() proto.Message

	// recvStream calls the given client with the request and returns a
	// receiver that reads the stream of responses.
	recvStream func(context.Context, proto.Message) (*receiver, func(), error)
}

// start executes the RPC call specified by this readStreamHandler using the
// specified serialized msg request.
func (s *readStreamHandler) start(msg []byte, rStream RecvStream) {
	// We must make a copy of the passed byte slice, as there is no
	// guarantee the contents won't be changed while the go routine is
	// executing.
	data := make([]byte, len(msg))
	copy(data[:], msg[:])

	go func() {
		// Get a new proto of the desired type and deserialize the
		// passed msg as this type.
		req := s.newProto()
		err := proto.Unmarshal(data, req)
		if err != nil {
			rStream.OnError(err)
			return
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		// Call the desired method on the client using the decoded gRPC
		// request, and get the receive stream back.
		stream, closeStream, err := s.recvStream(ctx, req)
		if err != nil {
			rStream.OnError(err)
			return
		}
		defer closeStream()

		// We will read responses from the stream until we encounter an
		// error.
		for {
			// Read a response from the stream.
			resp, err := stream.recv()
			if err != nil {
				rStream.OnError(err)
				return
			}

			// Serielize the response before returning it to the
			// caller.
			b, err := proto.Marshal(resp)
			if err != nil {
				rStream.OnError(err)
				return
			}
			rStream.OnResponse(b)
		}
	}()

}

// biStreamHandler is a struct used to call the daemon's RPC interface on
// methods where a bidirectional stream of request and responses is expected.
type biStreamHandler struct {
	// newProto returns an empty struct for the desired grpc request.
	newProto func() proto.Message

	// biStream calls the desired method on the given client and returns a
	// receiver that reads the stream of responses, and a sender that can
	// be used to send a stream of requests.
	biStream func(context.Context) (*receiver, *sender, func(), error)
}

// start executes the RPC call specified by this biStreamHandler, sending
// messages coming from the returned SendStream.
func (b *biStreamHandler) start(rStream RecvStream) (SendStream, error) {
	ctx, cancel := context.WithCancel(context.Background())

	// Start a bidirectional stream for the desired RPC method.
	r, s, closeStream, err := b.biStream(ctx)
	if err != nil {
		cancel()
		return nil, err
	}

	// We create a sendStream which is a wrapper for the methods we
	// will expose to the caller via the SendStream interface.
	ss := &sendStream{
		send: func(msg []byte) error {
			// Get an empty proto and deserialize the message
			// coming from the caller.
			req := b.newProto()
			err := proto.Unmarshal(msg, req)
			if err != nil {
				return err
			}

			// Send the request to the server.
			return s.send(req)
		},
		stop: s.closeStream,
	}

	// Now launch a goroutine that will handle the asynchronous stream of
	// responses.
	go func() {
		defer cancel()
		defer closeStream()

		// We will read responses from the recv stream until we
		// encounter an error.
		for {
			// Wait for a new response from the server.
			resp, err := r.recv()
			if err != nil {
				rStream.OnError(err)
				return
			}

			// Serialize the response before returning it to the
			// caller.
			b, err := proto.Marshal(resp)
			if err != nil {
				rStream.OnError(err)
				return
			}
			rStream.OnResponse(b)
		}
	}()

	// Return the send stream to the caller, which then can be used to pass
	// messages to the server.
	return ss, nil
}
`))
