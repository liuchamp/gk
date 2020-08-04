package main

import (
	"flag"
	"fmt"
	"net"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	stdzipkin "github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	stdprometheus "github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
	"github.com/go-kit/kit/metrics"
	"github.com/go-kit/kit/metrics/prometheus"
)

var (
	//版本
	GitHash   = "No GitHash Provided"
	BuildTime = "No BuildTime Provided"
)
var (
	gitHash   = flag.String("GitHash", GitHash, "Git hash code of this app (generate on build)")
	buildTime = flag.String("BuildTime", BuildTime, "Build time of this app (generate on build)")
)

var (
	//服务配置
	GrpcAddr  = flag.String("grpc-addr", ":10000", "gRPC listen address")
	DebugAddr = flag.String("debug-addr", ":10001", "Debug and metrics listen address")
	LogLevel  = flag.String("log-level", "debug", "Set log level (debug|info|warn|error)")
	SvcName   = flag.String("svc-name", "frontapi", "the name of this service in service discovery")
	//重试
	RetryMax     = flag.Int("retry-max", 1, "per-request retries to different instances")
	RetryTimeout = flag.Duration("retry-timeout", 30*time.Second, "per-request timeout, including retries")
	//服务依赖
	SvcDemo = flag.String("svc-demo", "demo:10010", "service name for demo server")
	//基础依赖
	ZipkinAddr = flag.String("zipkin-addr", "http://zipkin:9411/api/v2/spans", "Enable Zipkin tracing via a Zipkin HTTP Collector endpoint, http://zipkin:9411/api/v2/spans")
)

func main() {
	flag.Parse()

	// Logging domain.
	logger := buildLogger()
	logger.Log("msg", "hello", "gitHash", GitHash, "buildTime", BuildTime)
	defer logger.Log("msg", "goodbye", "gitHash", GitHash, "buildTime", BuildTime)

	var zipkinTracer *stdzipkin.Tracer
	{
		var (
			err           error
			hostPort      = "localhost:80"
			serviceName   = *SvcName
			useNoopTracer = (*ZipkinAddr == "")
			reporter      = zipkinhttp.NewReporter(*ZipkinAddr)
		)
		defer reporter.Close()
		zEP, _ := stdzipkin.NewEndpoint(serviceName, hostPort)
		sampler := stdzipkin.NewModuloSampler(10)
		zipkinTracer, err = stdzipkin.NewTracer(reporter, stdzipkin.WithLocalEndpoint(zEP), stdzipkin.WithNoopTracer(useNoopTracer), stdzipkin.WithSampler(sampler))
		if err != nil {
			_ = logger.Log("err", err)
			os.Exit(1)
		}
		if !useNoopTracer {
			_ = logger.Log("tracer", "Zipkin", "type", "Native", "URL", *ZipkinAddr)
		}
	}

	// Create the (sparse) metrics we'll use in the service. They, too, are
	// dependencies that we pass to components that use them.
	var (
		requestCount   metrics.Counter
		requestLatency metrics.Histogram
		duration       metrics.Histogram
		fieldKeys      []string
	)
	{
		// Business level metrics.
		fieldKeys = []string{"method", "error"}
		requestCount = prometheus.NewCounterFrom(stdprometheus.CounterOpts{
			Namespace: *SvcName,
			Name:      "request_count",
			Help:      "Number of requests received.",
		}, fieldKeys)
		requestLatency = prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
			Namespace: *SvcName,
			Name:      "request_latency_microseconds",
			Help:      "Total duration of requests in microseconds.",
		}, fieldKeys)

		// Transport level metrics.
		duration = prometheus.NewSummaryFrom(stdprometheus.SummaryOpts{
			Namespace: *SvcName,
			Name:      "request_duration_ns",
			Help:      "Request duration in nanoseconds.",
		}, []string{"method", "success"})
	}
	http.DefaultServeMux.Handle("/metrics", promhttp.Handler())

	// Interrupt handler.
	errc := make(chan error)
	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

	var basicService svcservice.Service
	{
		basicService = svcservice.NewBasicService(logger)
		basicService = svcservice.LoggingMiddleware(logger)(basicService)
		basicService = svcservice.InstrumentingMiddleware(requestCount, requestLatency)(basicService)
	}

	// gRPC transport.
	go func() {
		logger := log.With(logger, "transport", "gRPC")

		ln, err := net.Listen("tcp", *GrpcAddr)
		if err != nil {
			errc <- err
			return
		}
		endpoints := svcendpoint.New(basicService, logger, duration, zipkinTracer)
		grpcHandler := svctransport.NewGRPCServer(endpoints, zipkinTracer, logger)
		ka := keepalive.ServerParameters{}
		s := grpc.NewServer(grpc.KeepaliveParams(ka))
		svcpb.RegisterUserServer(s, grpcHandler)

		_ = logger.Log("addr", *GrpcAddr)
		errc <- s.Serve(ln)
	}()

	// Debug listener.
	go func() {
		logger := log.With(logger, "transport", "debug")
		m := http.NewServeMux()
		m.Handle("/debug/pprof/", http.HandlerFunc(pprof.Index))
		m.Handle("/debug/pprof/cmdline", http.HandlerFunc(pprof.Cmdline))
		m.Handle("/debug/pprof/profile", http.HandlerFunc(pprof.Profile))
		m.Handle("/debug/pprof/symbol", http.HandlerFunc(pprof.Symbol))
		m.Handle("/debug/pprof/trace", http.HandlerFunc(pprof.Trace))
		m.Handle("/metrics", promhttp.Handler())

		_ = logger.Log("addr", *DebugAddr)
		errc <- http.ListenAndServe(*DebugAddr, m)
	}()

	_ = logger.Log("terminated", <-errc)
}

func buildLogger() log.Logger {
	var logger log.Logger
	logLevel := level.AllowInfo()
	switch *LogLevel {
	case "debug":
		logLevel = level.AllowDebug()
	case "info":
		logLevel = level.AllowInfo()
	case "warn":
		logLevel = level.AllowWarn()
	case "error":
		logLevel = level.AllowError()
	}
	logger = log.NewLogfmtLogger(os.Stdout)
	logger = level.NewFilter(logger, logLevel)
	logger = log.With(logger, "svc", *SvcName)
	logTimeStamp := log.TimestampFormat(func() time.Time {
		return time.Now().In(time.FixedZone("CST", 60*60*8))
	}, "2006-01-02T15:04:05.000000")
	logger = log.With(logger, "ts", logTimeStamp)
	logger = log.With(logger, "caller", log.DefaultCaller)
	return logger
}
