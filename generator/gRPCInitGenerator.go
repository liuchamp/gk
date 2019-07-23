package generator

import (
	"errors"
	"fmt"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"github.com/yiv/gk/fs"
	"github.com/yiv/gk/parser"
	template "github.com/yiv/gk/templates"
	"github.com/yiv/gk/utils"
	"os"
	"strings"
)

type GRPCInitGenerator struct {
}

func NewGRPCInitGenerator() *GRPCInitGenerator {
	return &GRPCInitGenerator{}
}

func (sg *GRPCInitGenerator) Generate(name string) (err error) {
	te := template.NewEngine()
	defaultFs := fs.Get()

	var (
		path, fname, sfile string
		iface              *parser.Interface
		exist              bool
	)

	// pre-check
	{
		iface, err = LoadServiceInterfaceFromFile(name)

		if yes, err := IsProtoCompiled(name); err != nil {
			return err
		} else if !yes {
			err = errors.New("Could not find the compiled pb of the service")
			logrus.Error(err.Error())
			return err
		}
	}

	gosrc := utils.GetGOPATH() + "/src/"
	gosrc = strings.Replace(gosrc, "\\", "/", -1)
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}
	if viper.GetString("gk_folder") != "" {
		pwd += "/" + viper.GetString("gk_folder")
	}
	pwd = strings.Replace(pwd, "\\", "/", -1)
	projectPath := strings.Replace(pwd, gosrc, "", 1)
	pbImport := projectPath + "/" + path
	pbImport = strings.Replace(pbImport, "\\", "/", -1)
	enpointsPath, err := te.ExecuteString(viper.GetString("endpoints.path"), map[string]string{"ServiceName": name})
	if err != nil {
		return err
	}
	enpointsPath = strings.Replace(enpointsPath, "\\", "/", -1)
	endpointsImport := projectPath + "/" + enpointsPath

	handler := parser.NewFile()
	handler.Package = fmt.Sprintf("%stransport", name)
	handler.Imports = []parser.NamedTypeValue{
		parser.NewNameType("", "\"context\""),
		parser.NewNameType("", "\"errors\"\n"),
		parser.NewNameType("stdzipkin", `"github.com/openzipkin/zipkin-go"`),
		parser.NewNameType("stdopentracing", "\"github.com/opentracing/opentracing-go\""),
		parser.NewNameType("", "\"google.golang.org/grpc\""),
		parser.NewNameType("jujuratelimit", "\"github.com/juju/ratelimit\"\n"),
		parser.NewNameType("grpctransport", "\"github.com/go-kit/kit/transport/grpc\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/ratelimit\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/tracing/opentracing\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/endpoint\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/auth/jwt\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/log\"\n"),
		parser.NewNameType("", fmt.Sprintf("\"%s\"", pbImport)),
		parser.NewNameType("", fmt.Sprintf("\"%s\"", endpointsImport)),
	}

	if path, err = te.ExecuteString(viper.GetString("grpctransport.path"), map[string]string{"ServiceName": name}); err != nil {
		return err
	}
	if fname, err = te.ExecuteString(viper.GetString("grpctransport.file_name"), map[string]string{"ServiceName": name}); err != nil {
		return err
	}
	sfile = path + defaultFs.FilePathSeparator() + fname
	exist, err = defaultFs.Exists(sfile)
	if err != nil {
		return err
	}

	// If service generated before, go to update
	{
		if exist {
			logrus.Info("exist grpc transport file found: %v ", sfile)
			g := NewGRPCUpdateGenerator()
			err = g.Generate(name)
			return nil
		}
	}

	logrus.Info("Init grpc transport for service ", name)

	grpcStruct := parser.NewStruct("grpcServer", []parser.NamedTypeValue{})
	//NewGRPCServer
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"NewGRPCServer",
		`NewGRPCServer makes a set of endpoints available as a gRPC server.`,
		parser.NamedTypeValue{},
		`
		zipkinServer := zipkin.GRPCServerTrace(zipkinTracer)
		options := []grpctransport.ServerOption{
			grpctransport.ServerErrorHandler(transport.NewLogErrorHandler(logger)),
			zipkinServer,
		}
		gs := &grpcServer{}`,
		[]parser.NamedTypeValue{
			parser.NewNameType("endpoints", fmt.Sprintf("%sendpoint.Set", name)),
			parser.NewNameType("otTracer", "stdopentracing.Tracer"),
			parser.NewNameType("zipkinTracer", "*stdzipkin.Tracer"),
			parser.NewNameType("logger", "log.Logger"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", fmt.Sprintf("%spb.%sServer", name, utils.ToUpperFirstCamelCase(name))),
		},
	))
	//NewGRPCClient
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"NewGRPCClient",
		`NewGRPCClient makes a set of endpoints available as a gRPC client.`,
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		zipkinClient := zipkin.GRPCClientTrace(zipkinTracer)
		options := []grpctransport.ClientOption{
			zipkinClient,
		}
		set := %sendpoint.Set{}
		`, name),
		[]parser.NamedTypeValue{
			parser.NewNameType("conn", "*grpc.ClientConn"),
			parser.NewNameType("otTracer", "stdopentracing.Tracer"),
			parser.NewNameType("zipkinTracer", "*stdzipkin.Tracer"),
			parser.NewNameType("logger", "log.Logger"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", fmt.Sprintf("%sservice.Service", utils.ToLowerFirstCamelCase(name))),
		},
	))
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"str2err",
		`str2err `,
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		if s == "" {
			return nil
		}
		return errors.New(s)
		`),
		[]parser.NamedTypeValue{
			parser.NewNameType("s", "string"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "error"),
		},
	))
	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"err2str",
		`err2str `,
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		if err == nil {
			return ""
		}
		return err.Error()
		`),
		[]parser.NamedTypeValue{
			parser.NewNameType("err", "error"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "string"),
		},
	))
	for _, v := range iface.Methods {
		//add member to grpcServer
		grpcStruct.Vars = append(grpcStruct.Vars, parser.NewNameType(
			utils.ToLowerFirstCamelCase(v.Name),
			"grpctransport.Handler",
		))

		// add server side request decoder
		var decodeReqParamList string
		for _, v := range v.Parameters {
			if v.Type == "context.Context" {
				continue
			}
			pname := utils.ToUpperFirstCamelCase(v.Name)
			decodeReqParamList += fmt.Sprintf("%s:r.%s,", pname, pname)
		}
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"decodeGRPC"+v.Name+"Req",
			fmt.Sprintf(
				`DecodeGRPC%sRequest is a transport/grpc.DecodeRequestFunc that converts a
				gRPC request to a user-domain request. Primarily useful in a server.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`r := grpcReq.(*%spb.%sReq)
			req := %sendpoint.%sReq{%s}
			return req, nil`, name, v.Name, name, v.Name, decodeReqParamList),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("grpcReq", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))

		// add server side response encoder
		var encodeResParamList string
		for _, v := range v.Results {
			pname := utils.ToUpperFirstCamelCase(v.Name)
			if v.Name == "Err" || v.Name == "err" || v.Type == "error" || v.Type == "Error" {
				encodeResParamList += fmt.Sprintf("%s:err2str(r.%s),", pname, pname)
			} else {
				encodeResParamList += fmt.Sprintf("%s:r.%s,", pname, pname)
			}
		}
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"encodeGRPC"+v.Name+"Res",
			fmt.Sprintf(
				`EncodeGRPC%sResponse is a transport/grpc.EncodeResponseFunc that converts a
					user-domain response to a gRPC reply. Primarily useful in a server.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`r := response.(%sendpoint.%sRes)
			res := &%spb.%sRes{%s}
			return res, nil`, name, v.Name, name, v.Name, encodeResParamList),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("response", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))

		// add client side request encoder
		var encodeReqParamList string
		for _, v := range v.Parameters {
			if v.Type == "context.Context" {
				continue
			}
			pname := utils.ToUpperFirstCamelCase(v.Name)
			encodeReqParamList += fmt.Sprintf("%s:r.%s,", pname, pname)
		}
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"encodeGRPC"+v.Name+"Req",
			fmt.Sprintf(
				`encodeGRPC%Req s a transport/grpc.EncodeRequestFunc that converts a
				 user-domain sum request to a gRPC sum request. Primarily useful in a client.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`r := request.(%sendpoint.%sReq)
			req :=  &%spb.%sReq{%s}
			return req, nil`, name, v.Name, name, v.Name, encodeReqParamList),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("request", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))

		// add client side response decoder
		var decodeResParamList string
		for _, v := range v.Results {
			pname := utils.ToUpperFirstCamelCase(v.Name)
			if v.Name == "Err" || v.Name == "err" || v.Type == "error" || v.Type == "Error" {
				decodeResParamList += fmt.Sprintf("%s:str2err(r.%s),", pname, pname)
			} else {
				decodeResParamList += fmt.Sprintf("%s:r.%s,", pname, pname)
			}
		}
		handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
			"decodeGRPC"+v.Name+"Res",
			fmt.Sprintf(
				`decodeGRPC%Res is a transport/grpc.DecodeResponseFunc that converts a
				 gRPC sum reply to a user-domain sum response. Primarily useful in a client.`,
				v.Name,
			),
			parser.NamedTypeValue{},
			fmt.Sprintf(`r := grpcReply.(*%spb.%sRes)
			res := %sendpoint.%sRes{%s}
			return res, nil`, name, utils.ToUpperFirstCamelCase(v.Name), name, v.Name, decodeResParamList),
			[]parser.NamedTypeValue{
				parser.NewNameType("_", "context.Context"),
				parser.NewNameType("grpcReply", "interface{}"),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("", "interface{}"),
				parser.NewNameType("", "error"),
			},
		))
		//add interface method
		handler.Methods = append(handler.Methods, parser.NewMethod(
			v.Name,
			parser.NewNameType("s", "*grpcServer"),
			fmt.Sprintf(
				`_, rp, err := s.%s.ServeGRPC(ctx, req)
					if err != nil {
						return nil, err
					}
					rep = rp.(*%spb.%sRes)
					return rep, err`,
				utils.ToLowerFirstCamelCase(v.Name),
				name,
				v.Name,
			),
			[]parser.NamedTypeValue{
				parser.NewNameType("ctx", "context.Context"),
				parser.NewNameType("req", fmt.Sprintf("*%spb.%sReq", name, v.Name)),
			},
			[]parser.NamedTypeValue{
				parser.NewNameType("rep", fmt.Sprintf("*%spb.%sRes", name, v.Name)),
				parser.NewNameType("err", "error"),
			},
		))

		//init grpcServer method
		handler.Methods[0].Body += "\n" + fmt.Sprintf(`
			gs.%s = grpctransport.NewServer(
			endpoints.%sEndpoint,
			decodeGRPC%sReq,
			encodeGRPC%sRes,
			append(
				options,
				grpctransport.ServerBefore(opentracing.GRPCToContext(otTracer, "%s", logger)),
				grpctransport.ServerBefore(jwt.GRPCToContext()),
			)...,
		)
		`, utils.ToLowerFirstCamelCase(v.Name), v.Name, v.Name, v.Name, v.Name)

		//init grpc client method
		lowerName := utils.ToLowerFirstCamelCase(v.Name)
		upperName := utils.ToUpperFirstCamelCase(v.Name)
		handler.Methods[1].Body += "\n" + fmt.Sprintf(`
			var %sEndpoint endpoint.Endpoint
			{
				%sEndpoint = grpctransport.NewClient(
					conn,
					"%spb.%s",
					"%s",
					encodeGRPC%sReq,
					decodeGRPC%sRes,
					%spb.%sRes{},
					append(options, 
						grpctransport.ClientBefore(opentracing.ContextToGRPC(otTracer, logger)),
						grpctransport.ClientBefore(jwt.ContextToGRPC()),
					)...,
				).Endpoint()
				%sEndpoint = opentracing.TraceClient(otTracer, "%s")(%sEndpoint)
				set.%sEndpoint = %sEndpoint
			}
		`, lowerName, lowerName,
			name,
			utils.ToUpperFirstCamelCase(name), upperName, upperName, upperName, name, upperName,
			lowerName, lowerName, lowerName,
			upperName,
			lowerName)
	}
	//close NewGRPCServer
	handler.Methods[0].Body += `
	return gs`
	//close NewGRPCClient
	handler.Methods[1].Body += `
	return set`

	handler.Structs = append(handler.Structs, grpcStruct)

	err = defaultFs.WriteFile(sfile, handler.String(), false)
	if err != nil {
		return err
	}
	logrus.Warn("---------------------------------------------------------------------------------------")
	logrus.Warn("The generator does not implement the Decoding and Encoding of the grpc request/response")
	logrus.Warn("Before using the service don't forget to implement those.")
	logrus.Warn("---------------------------------------------------------------------------------------")
	return nil
}

func (sg *GRPCInitGenerator) GenerateEndpointClient(name string) (err error) {
	te := template.NewEngine()
	defaultFs := fs.Get()

	var (
		path, fname, sfile string
		iface              *parser.Interface
		exist              bool
	)

	// pre-check
	{
		iface, err = LoadServiceInterfaceFromFile(name)

		if yes, err := IsProtoCompiled(name); err != nil {
			return err
		} else if !yes {
			return errors.New("Could not find the compiled pb of the service")
		}
	}
	handler := parser.NewFile()

	gosrc := utils.GetGOPATH() + "/src/"
	gosrc = strings.Replace(gosrc, "\\", "/", -1)
	pwd, err := os.Getwd()
	if err != nil {
		logrus.Error(err.Error())
		return err
	}
	if viper.GetString("gk_folder") != "" {
		pwd += "/" + viper.GetString("gk_folder")
	}
	pwd = strings.Replace(pwd, "\\", "/", -1)
	projectPath := strings.Replace(pwd, gosrc, "", 1)
	pbImport := projectPath + "/" + path
	pbImport = strings.Replace(pbImport, "\\", "/", -1)
	enpointsPath, err := te.ExecuteString(viper.GetString("endpoints.path"), map[string]string{"ServiceName": name})
	if err != nil {
		logrus.Error(err.Error())
		return err
	}
	enpointsPath = strings.Replace(enpointsPath, "\\", "/", -1)
	endpointsImport := projectPath + "/" + enpointsPath

	handler.Package = fmt.Sprintf("%stransport", name)
	handler.Imports = []parser.NamedTypeValue{
		parser.NewNameType("", "\"io\""),
		parser.NewNameType("", "\"time\"\n"),

		parser.NewNameType("stdzipkin", `"github.com/openzipkin/zipkin-go"`),
		parser.NewNameType("stdopentracing", "\"github.com/opentracing/opentracing-go\""),
		parser.NewNameType("", "\"google.golang.org/grpc\"\n"),

		parser.NewNameType("", "\"github.com/go-kit/kit/sd\""),
		parser.NewNameType("ketcd", "\"github.com/go-kit/kit/sd/etcd\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/endpoint\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/sd/lb\""),
		parser.NewNameType("", "\"github.com/go-kit/kit/log\"\n"),

		parser.NewNameType("", fmt.Sprintf("\"%s\"", pbImport)),
		parser.NewNameType("", fmt.Sprintf("\"%s\"", endpointsImport)),
	}

	if path, err = te.ExecuteString(viper.GetString("grpctransport.path"), map[string]string{"ServiceName": name}); err != nil {
		logrus.Error(err.Error())
		return err
	}
	if fname, err = te.ExecuteString(viper.GetString("grpctransport.client_file_name"), map[string]string{"ServiceName": name}); err != nil {
		logrus.Error(err.Error())
		return err
	}
	sfile = path + defaultFs.FilePathSeparator() + fname
	exist, err = defaultFs.Exists(sfile)
	if err != nil {
		logrus.Error(err.Error())
		return err
	}

	// If service generated before, go to update
	{
		if exist {
			g := NewGRPCUpdateGenerator()
			err = g.UpdateEndpointClient(name)
			return nil
		}
	}

	logrus.Info("Init client of grpc endpoint for service ", name)

	handler.Methods = append(handler.Methods, parser.NewMethodWithComment(
		"NewEndpointClientSet",
		`NewEndpointClientSet makes a set of endpoints available for a gRPC client.`,
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		var instancer *ketcd.Instancer
		if instancer, err = ketcd.NewInstancer(etcdClient, svcName, logger); err != nil {
			panic(err.Error())
		}
		set = %sendpoint.Set{}
		`, name),
		[]parser.NamedTypeValue{
			parser.NewNameType("svcName", "string"),
			parser.NewNameType("retryMax", "int"),
			parser.NewNameType("retryTimeout", "time.Duration"),
			parser.NewNameType("logger", "log.Logger"),
			parser.NewNameType("etcdClient", "ketcd.Client"),
			parser.NewNameType("otTracer", "stdopentracing.Tracer"),
			parser.NewNameType("zipkinTracer", "*stdzipkin.Tracer"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("set", fmt.Sprintf("%sendpoint.Set", name)),
			parser.NewNameType("err", "error"),
		},
	))

	handler.Methods = append(handler.Methods, parser.NewMethod(
		"factory",
		parser.NamedTypeValue{},
		fmt.Sprintf(`
		return func(instance string) (endpoint.Endpoint, io.Closer, error) {
			conn, err := grpc.Dial(instance, grpc.WithInsecure())
			if err != nil {
				return nil, nil, err
			}
			service := NewGRPCClient(conn, otTracer, zipkinTracer, logger)
			ep := makeEndpoint(service)
	
			return ep, conn, nil
		}
		`),
		[]parser.NamedTypeValue{
			parser.NewNameType("makeEndpoint", fmt.Sprintf("func(%sservice.Service) endpoint.Endpoint", name)),
			parser.NewNameType("otTracer", "stdopentracing.Tracer"),
			parser.NewNameType("zipkinTracer", "*stdzipkin.Tracer"),
			parser.NewNameType("logger", "log.Logger"),
		},
		[]parser.NamedTypeValue{
			parser.NewNameType("", "sd.Factory"),
		},
	))

	for _, v := range iface.Methods {
		handler.Methods[0].Body += "\n" + fmt.Sprintf(`
		{
			factory := factory(%sendpoint.Make%sEndpoint, otTracer, zipkinTracer, logger)
			endpointer := sd.NewEndpointer(instancer, factory, logger)
			balancer := lb.NewRoundRobin(endpointer)
			retry := lb.Retry(retryMax, retryTimeout, balancer)
			set.%sEndpoint = retry
		}
		`, name, utils.ToUpperFirstCamelCase(v.Name), utils.ToUpperFirstCamelCase(v.Name))
	}

	handler.Methods[0].Body += `
	return `

	err = defaultFs.WriteFile(sfile, handler.String(), false)
	if err != nil {
		logrus.Error(err.Error())
		return err
	}

	return
}
