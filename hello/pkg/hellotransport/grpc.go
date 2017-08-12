package hellotransport

import (
	"context"

	jujuratelimit "github.com/juju/ratelimit"
	stdopentracing "github.com/opentracing/opentracing-go"
	oldcontext "golang.org/x/net/context"
	"google.golang.org/grpc"

	"github.com/go-kit/kit/auth/jwt"
	"github.com/go-kit/kit/endpoint"
	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/ratelimit"
	"github.com/go-kit/kit/tracing/opentracing"
	grpctransport "github.com/go-kit/kit/transport/grpc"

	"github.com/yiv/gk/hello/pb"
	"github.com/yiv/gk/hello/pkg/helloendpoint"
	"github.com/yiv/gk/hello/pkg/helloservice"
)

type grpcServer struct {
	foo grpctransport.Handler
}

// NewGRPCServer makes a set of endpoints available as a gRPC server.
func NewGRPCServer(endpoints helloendpoint.Set, tracer stdopentracing.Tracer, logger log.Logger) (req pb.HelloServer) {
	options := []grpctransport.ServerOption{
		grpctransport.ServerErrorLogger(logger),
	}
	req = &grpcServer{
		foo: grpctransport.NewServer(
			endpoints.FooEndpoint,
			decodeGRPCFooReq,
			encodeGRPCFooRes,
			append(
				options,
				grpctransport.ServerBefore(opentracing.GRPCToContext(tracer, "Foo", logger)),
				grpctransport.ServerBefore(jwt.GRPCToContext()),
			)...,
		),
	}
	return req
}

// NewGRPCClient makes a set of endpoints available as a gRPC client.
func NewGRPCClient(conn *grpc.ClientConn, tracer stdopentracing.Tracer, logger log.Logger) helloservice.Service {
	set := helloendpoint.Set{}
	limiter := ratelimit.NewTokenBucketLimiter(jujuratelimit.NewBucketWithRate(100, 100))

	var fooEndpoint endpoint.Endpoint
	{
		fooEndpoint = grpctransport.NewClient(
			conn,
			"pb.Hello",
			"Foo",
			encodeGRPCFooReq,
			decodeGRPCFooRes,
			pb.FooRes{},
			grpctransport.ClientBefore(opentracing.ContextToGRPC(tracer, logger)),
			grpctransport.ClientBefore(jwt.ContextToGRPC()),
		).Endpoint()
		fooEndpoint = opentracing.TraceClient(tracer, "foo")(fooEndpoint)
		fooEndpoint = limiter(fooEndpoint)
		set.FooEndpoint = fooEndpoint
	}

	return set
}

// DecodeGRPCFooRequest is a transport/grpc.DecodeRequestFunc that converts a
// gRPC request to a user-domain request. Primarily useful in a server.
func decodeGRPCFooReq(_ context.Context, grpcReq interface{}) (req interface{}, err error) {
	r := grpcReq.(*pb.FooReq)
	return req, err
}

// EncodeGRPCFooResponse is a transport/grpc.EncodeResponseFunc that converts a
// user-domain response to a gRPC reply. Primarily useful in a server.
func encodeGRPCFooRes(_ context.Context, response interface{}) (res interface{}, err error) {
	r := response.(helloendpoint.FooRes)
	return res, err
}

// encodeGRPC%!R(string=Foo)eq s a transport/grpc.EncodeRequestFunc that converts a
// user-domain sum request to a gRPC sum request. Primarily useful in a client.
func encodeGRPCFooReq(_ context.Context, request interface{}) (req interface{}, err error) {
	r := request.(helloendpoint.FooReq)
	return req, err
}

// decodeGRPC%!R(string=Foo)es is a transport/grpc.DecodeResponseFunc that converts a
// gRPC sum reply to a user-domain sum response. Primarily useful in a client.
func decodeGRPCFooRes(_ context.Context, grpcReply interface{}) (res interface{}, err error) {
	r := grpcReply.(*pb.FooRes)
	return res, err
}

func (s *grpcServer) Foo(ctx oldcontext.Context, req *pb.FooReq) (rep *pb.FooRes, err error) {
	_, rp, err := s.foo.ServeGRPC(ctx, req)
	if err != nil {
		return nil, err
	}
	rep = rp.(*pb.FooRes)
	return rep, err
}
