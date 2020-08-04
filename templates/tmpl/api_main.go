package main

import (
	"flag"
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gorilla/mux"
	stdzipkin "github.com/openzipkin/zipkin-go"
	zipkinhttp "github.com/openzipkin/zipkin-go/reporter/http"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/go-kit/kit/log"
	"github.com/go-kit/kit/log/level"
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
	HttpAddr  = flag.String("http-addr", ":10000", "Address for HTTP (JSON) server")
	DebugAddr = flag.String("debug-addr", ":10001", "Debug and metrics listen address")
	LogLevel  = flag.String("log-level", "debug", "Set log level (debug|info|warn|error)")
	SvcName   = flag.String("svc-name", "frontapi", "the name of this service in service discovery")
	//重试
	RetryMax     = flag.Int("retry-max", 1, "per-request retries to different instances")
	RetryTimeout = flag.Duration("retry-timeout", 30*time.Second, "per-request timeout, including retries")
	//服务依赖
	SvcDemo = flag.String("svc-demo", "demo", "service name for demo server")
	//基础依赖
	ZipkinAddr = flag.String("zipkin-addr", "http://zipkin:9411/api/v2/spans", "Enable Zipkin tracing via a Zipkin HTTP Collector endpoint, http://zipkin:9411/api/v2/spans")
)

func main() {
	flag.Parse()

	// Logging domain.
	logger := buildLogger()
	logger.Log("msg", "hello", "gitHash", GitHash, "buildTime", BuildTime)
	defer logger.Log("msg", "goodbye", "gitHash", GitHash, "buildTime", BuildTime)

	// Interrupt handler.
	errc := make(chan error)
	go func() {
		c := make(chan os.Signal)
		signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)
		errc <- fmt.Errorf("%s", <-c)
	}()

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
			logger.Log("err", err)
			os.Exit(1)
		}
		if !useNoopTracer {
			logger.Log("tracer", "Zipkin", "type", "Native", "URL", *ZipkinAddr)
		}

	}

	// Server routes.
	r := mux.NewRouter()
	{

		//{
		//	var userSet userendpoint.Set
		//	if userSet, err = usertransport.NewEndpointClientSet(*SvcUser, *RetryMax, *RetryTimeout, logger,  zipkinTracer); err != nil {
		//		panic(err.Error())
		//	}
		//	r.PathPrefix("/app/user").Handler(http.StripPrefix("/api/user", accessControl(usertransport.NewHTTPHandler(userSet, otTracer, zipkinTracer, logger))))
		//}
	}

	// HTTP transport.
	go func() {
		logger.Log("transport1", "HTTP", "addr", *HttpAddr)
		errc <- http.ListenAndServe(*HttpAddr, r)
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

		logger.Log("addr", *DebugAddr)
		errc <- http.ListenAndServe(*DebugAddr, m)
	}()

	_ = logger.Log("terminated", <-errc)
}

func accessControl(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		allowOrigin := r.Header.Get("Origin")
		if allowOrigin == "" {
			allowOrigin = "*"
		}
		w.Header().Set("Access-Control-Allow-Origin", allowOrigin)

		w.Header().Set("Access-Control-Allow-Methods", "GET,PUT,POST,DELETE,OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Authorization")

		h.ServeHTTP(w, r)
	})
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
