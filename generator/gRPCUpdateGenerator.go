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
	"strings"
)

type GRPCUpdateGenerator struct {
}

func NewGRPCUpdateGenerator() *GRPCUpdateGenerator {
	return &GRPCUpdateGenerator{}
}

func (sg *GRPCUpdateGenerator) Generate(name string) error {
	logrus.Info("Updating grpc transport for service ", name)
	te := template.NewEngine()
	defaultFs := fs.Get()
	path, err := te.ExecuteString(viper.GetString("service.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	fname, err := te.ExecuteString(viper.GetString("service.file_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile := path + defaultFs.FilePathSeparator() + fname
	b, err := defaultFs.Exists(sfile)
	if err != nil {
		return err
	}
	iname, err := te.ExecuteString(viper.GetString("service.interface_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	if !b {
		return errors.New(fmt.Sprintf("Service %s was not found", name))
	}
	p := parser.NewFileParser()
	s, err := defaultFs.ReadFile(sfile)
	if err != nil {
		return err
	}
	svcFile, err := p.Parse([]byte(s))
	if err != nil {
		return err
	}

	var iface *parser.Interface
	for _, v := range svcFile.Interfaces {
		if v.Name == iname {
			iface = &v
		}
	}
	if iface == nil {
		return errors.New(fmt.Sprintf("Could not find the service interface in `%s`", sfile))
	}
	toKeep := []parser.Method{}
	for _, v := range iface.Methods {
		isOk := false
		for _, p := range v.Parameters {
			if p.Type == "context.Context" {
				isOk = true
				break
			}
		}
		if string(v.Name[0]) == strings.ToLower(string(v.Name[0])) {
			logrus.Warnf("The method '%s' is private and will be ignored", v.Name)
			continue
		}
		if len(v.Results) == 0 {
			logrus.Warnf("The method '%s' does not have any return value and will be ignored", v.Name)
			continue
		}
		if !isOk {
			logrus.Warnf("The method '%s' does not have a context and will be ignored", v.Name)
		}
		if isOk {
			toKeep = append(toKeep, v)
		}

	}
	iface.Methods = toKeep
	if len(iface.Methods) == 0 {
		return errors.New("The service has no method please implement the interface methods")
	}
	path, err = te.ExecuteString(viper.GetString("pb.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile = path + defaultFs.FilePathSeparator() + utils.ToLowerSnakeCase(name) + ".pb.go"
	b, err = defaultFs.Exists(sfile)
	if err != nil {
		return err
	}
	if !b {
		return errors.New("Could not find the compiled pb of the service")
	}

	path, err = te.ExecuteString(viper.GetString("grpctransport.path"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	fname, err = te.ExecuteString(viper.GetString("grpctransport.file_name"), map[string]string{
		"ServiceName": name,
	})
	if err != nil {
		return err
	}
	sfile = path + defaultFs.FilePathSeparator() + fname

	s, err = defaultFs.ReadFile(sfile)
	if err != nil {
		return err
	}
	var handler *parser.File
	handler, err = p.Parse([]byte(s))
	if err != nil {
		return err
	}

	var grpcServer *parser.Struct
	for k, v := range handler.Structs {
		if v.Name == "grpcServer" {
			grpcServer = &handler.Structs[k]
			break
		}
	}
	handler.Methods[0].Body = strings.ReplaceAll(handler.Methods[0].Body, "return gs", "")
	handler.Methods[1].Body = strings.ReplaceAll(handler.Methods[1].Body, "return set", "")

	if grpcServer == nil {
		err = errors.New("Could not find grpcStruct")
		logrus.Error(err)
		return err
	}

	for _, v := range iface.Methods {
		var isExist bool
		for _, vv := range grpcServer.Vars {
			if vv.Name == utils.ToLowerFirstCamelCase(v.Name) {
				isExist = true
				break
			}
		}
		if isExist {
			continue
		}
		//add member to grpcServer
		grpcServer.Vars = append(grpcServer.Vars, parser.NewNameType(
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
				%sEndpoint = limiter(%sEndpoint)
				%sEndpoint = circuitbreaker.Gobreaker(gobreaker.NewCircuitBreaker(gobreaker.Settings{
					Name:    "%s",
					Timeout: 10 * time.Second,
				}))(%sEndpoint)
				set.%sEndpoint = %sEndpoint
			}
		`, lowerName, lowerName,
			name,
			utils.ToUpperFirstCamelCase(name), upperName, upperName, upperName, name, upperName,
			lowerName, lowerName, lowerName, lowerName, lowerName, lowerName,
			upperName,
			lowerName,
			upperName,
			lowerName)
	}
	//close NewGRPCServer
	handler.Methods[0].Body += `
	return gs`
	//close NewGRPCClient
	handler.Methods[1].Body += `
	return set`

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
